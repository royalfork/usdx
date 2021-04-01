//go:generate abigen --sol ../../sol/MockOracle.sol --pkg usdx --out mock_oracle_abigen.go
// NOTE: Using forked abigen from: https://github.com/ethereum/go-ethereum/pull/21938
package usdx

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/royalfork/soltest"
)

func TestMockOracle(t *testing.T) {
	chain, accts := soltest.New()

	_, _, contract, err := DeployMockOracle(accts[0].Auth, chain)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	var (
		zero = new(big.Int)

		decimals    = uint8(8)
		rate        = big.NewInt(58973604819)
		updatedTime = big.NewInt(1606332547)
	)

	if !chain.Succeed(contract.SetLastRound(accts[0].Auth, zero, rate, zero, updatedTime, zero)) {
		t.Fatal("unable to set last round data")
	}

	t.Run("readDecimals", func(t *testing.T) {
		if decs, err := contract.Decimals(&bind.CallOpts{}); err != nil {
			t.Errorf("unexpected read err: %v", err)
		} else if decs != decimals {
			t.Errorf("want: %d, got: %d", decimals, decs)
		}
	})

	t.Run("readLastRoundData", func(t *testing.T) {
		if roundData, err := contract.LatestRoundData(&bind.CallOpts{}); err != nil {
			t.Errorf("unexpected read err: %+v", err)
		} else if roundData.Answer.Cmp(rate) != 0 {
			t.Errorf("want answer: %v, got: %v", rate, roundData.Answer)
		} else if roundData.UpdatedAt.Cmp(updatedTime) != 0 {
			t.Errorf("want updatedAt: %v, got: %v", updatedTime, roundData.UpdatedAt)
		}
	})

	t.Run("accessDenied", func(t *testing.T) {
		if !chain.Succeed(contract.SetAccess(accts[0].Auth, false)) {
			t.Fatal("unable to deny access")
		}
		if _, err := contract.LatestRoundData(&bind.CallOpts{}); err == nil {
			t.Error("expected err when reading latest round data")
		}
	})
}
