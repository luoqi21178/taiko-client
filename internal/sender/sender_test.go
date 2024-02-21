package sender_test

import (
	"context"
	"math/big"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"

	"github.com/taikoxyz/taiko-client/internal/sender"
	"github.com/taikoxyz/taiko-client/internal/utils"
	"github.com/taikoxyz/taiko-client/pkg/rpc"
)

func setSender(cfg *sender.Config) (*rpc.EthClient, *sender.Sender, error) {
	ctx := context.Background()

	client, err := rpc.NewEthClient(ctx, os.Getenv("L1_NODE_WS_ENDPOINT"), time.Second*10)
	if err != nil {
		return nil, nil, err
	}

	priv, err := crypto.ToECDSA(common.FromHex(os.Getenv("L1_PROPOSER_PRIVATE_KEY")))
	if err != nil {
		return nil, nil, err
	}

	send, err := sender.NewSender(ctx, cfg, client, priv)

	return client, send, err
}

func TestNormalSender(t *testing.T) {
	utils.LoadEnv()
	_, send, err := setSender(&sender.Config{
		Confirmations: 0,
		MaxGasPrice:   big.NewInt(20000000000),
		GasRate:       50,
		MaxPendTxs:    10,
		RetryTimes:    0,
	})
	assert.NoError(t, err)
	defer send.Stop()

	var (
		batchSize  = 5
		eg         errgroup.Group
		confirmsCh = make([]<-chan *sender.TxConfirm, 0, batchSize)
	)
	eg.SetLimit(runtime.NumCPU())
	for i := 0; i < batchSize; i++ {
		i := i
		eg.Go(func() error {
			addr := common.BigToAddress(big.NewInt(int64(i)))
			txID, err := send.SendRaw(send.Opts.Nonce.Uint64(), &addr, big.NewInt(1), nil)
			if err == nil {
				confirmCh, _ := send.WaitTxConfirm(txID)
				confirmsCh = append(confirmsCh, confirmCh)
			}
			return err
		})
	}
	err = eg.Wait()
	assert.NoError(t, err)

	for ; len(confirmsCh) > 0; confirmsCh = confirmsCh[1:] {
		confirm := <-confirmsCh[0]
		assert.NoError(t, confirm.Error)
	}
}

// Test touch max gas price and replacement.
func TestReplacement(t *testing.T) {
	utils.LoadEnv()

	client, send, err := setSender(&sender.Config{
		Confirmations: 0,
		MaxGasPrice:   big.NewInt(20000000000),
		GasRate:       50,
		MaxPendTxs:    10,
		RetryTimes:    0,
	})
	assert.NoError(t, err)
	defer send.Stop()

	// Let max gas price be 2 times of the gas fee cap.
	send.MaxGasPrice = new(big.Int).Mul(send.Opts.GasFeeCap, big.NewInt(2))

	nonce, err := client.NonceAt(context.Background(), send.Opts.From, nil)
	assert.NoError(t, err)

	pendingNonce, err := client.PendingNonceAt(context.Background(), send.Opts.From)
	assert.NoError(t, err)
	// Run test only if mempool has no pending transactions.
	if pendingNonce > nonce {
		return
	}

	nonce++
	baseTx := &types.DynamicFeeTx{
		ChainID:   send.ChainID,
		To:        &common.Address{},
		GasFeeCap: send.MaxGasPrice,
		GasTipCap: send.MaxGasPrice,
		Nonce:     nonce,
		Gas:       21000,
		Value:     big.NewInt(1),
		Data:      nil,
	}
	rawTx, err := send.Opts.Signer(send.Opts.From, types.NewTx(baseTx))
	assert.NoError(t, err)
	err = client.SendTransaction(context.Background(), rawTx)
	assert.NoError(t, err)

	confirmsCh := make([]<-chan *sender.TxConfirm, 0, 5)

	// Replace the transaction with a higher nonce.
	txID, err := send.SendRaw(nonce, &common.Address{}, big.NewInt(1), nil)
	assert.NoError(t, err)
	confirmCh, _ := send.WaitTxConfirm(txID)
	confirmsCh = append(confirmsCh, confirmCh)

	time.Sleep(time.Second * 6)
	// Send a transaction with a next nonce and let all the transactions be confirmed.
	txID, err = send.SendRaw(nonce-1, &common.Address{}, big.NewInt(1), nil)
	assert.NoError(t, err)
	confirmCh, _ = send.WaitTxConfirm(txID)
	confirmsCh = append(confirmsCh, confirmCh)

	for ; len(confirmsCh) > 0; confirmsCh = confirmsCh[1:] {
		confirm := <-confirmsCh[0]
		// Check the replaced transaction's gasFeeTap touch the max gas price.
		if confirm.Tx.Nonce() == nonce {
			assert.Equal(t, send.MaxGasPrice, confirm.Tx.GasFeeCap())
		}
		assert.NoError(t, confirm.Error)
		t.Log(confirm.Receipt.BlockNumber.String())
	}

	_, err = client.TransactionReceipt(context.Background(), rawTx.Hash())
	assert.Equal(t, "not found", err.Error())
}

// Test nonce too low.
func TestNonceTooLow(t *testing.T) {
	utils.LoadEnv()

	client, send, err := setSender(&sender.Config{
		Confirmations: 0,
		MaxGasPrice:   big.NewInt(20000000000),
		GasRate:       50,
		MaxPendTxs:    10,
		RetryTimes:    0,
	})
	assert.NoError(t, err)
	defer send.Stop()

	nonce, err := client.NonceAt(context.Background(), send.Opts.From, nil)
	assert.NoError(t, err)
	pendingNonce, err := client.PendingNonceAt(context.Background(), send.Opts.From)
	assert.NoError(t, err)
	// Run test only if mempool has no pending transactions.
	if pendingNonce > nonce {
		return
	}

	txID, err := send.SendRaw(nonce-3, &common.Address{}, big.NewInt(1), nil)
	assert.NoError(t, err)
	confirmCh, _ := send.WaitTxConfirm(txID)
	confirm := <-confirmCh
	assert.NoError(t, confirm.Error)
	assert.Equal(t, nonce, confirm.Tx.Nonce())
}
