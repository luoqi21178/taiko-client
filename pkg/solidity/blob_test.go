package solidity

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"

	"github.com/taikoxyz/taiko-client/internal/utils"
	"github.com/taikoxyz/taiko-client/pkg/rpc"
)

func TestBlob(t *testing.T) {
	utils.LoadEnv()
	ctx := context.Background()

	url := "ws://localhost:8546" //os.Getenv("L1_NODE_WS_ENDPOINT")
	client, err := rpc.NewEthClient(ctx, url, time.Second*20)
	assert.NoError(t, err)

	sk, err := crypto.ToECDSA(common.FromHex("0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"))
	assert.NoError(t, err)

	chainID, err := client.ChainID(ctx)
	assert.NoError(t, err)

	opts, err := bind.NewKeyedTransactorWithChainID(sk, chainID)
	assert.NoError(t, err)

	addr, tx, token, _ := DeployBallotTest(opts, client)
	_, err = bind.WaitMined(ctx, client, tx)
	assert.NoError(t, err)

	t.Log("blob test address", "address", addr.String())

	opts.NoSend = true
	opts.GasLimit = 1000000
	tx, err = token.StoreBlobHash(opts)
	assert.NoError(t, err)
	input := tx.Data()

	sideCar, err := rpc.MakeSidecarWithSingleBlob([]byte("s"))

	opts.NoSend = false
	opts.GasLimit = 0
	tx, err = client.TransactBlobTx(opts, &addr, input, sideCar)
	assert.Error(t, err)
	t.Logf("can't get blob hash, err: %v", err)

	opts.GasLimit = 1000000
	blobTx, err := client.TransactBlobTx(opts, &addr, input, sideCar)
	assert.NoError(t, err)

	receipt, err := bind.WaitMined(ctx, client, blobTx)
	assert.NoError(t, err)
	assert.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)

	t.Logf("send blob tx successful, number: %d, tx_hash: %s", receipt.BlockNumber.Uint64(), blobTx.Hash())
}
