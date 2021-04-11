//go:generate abigen --sol ../../sol/Usdx.sol --pkg usdx --out usdx_abigen.go --exc ../../sol/chainlink/evm-contracts/src/v0.7/interfaces/AggregatorV3Interface.sol:AggregatorV3Interface
package usdx

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/royalfork/soltest"
)

var (
	zero = new(big.Int)
	rate = big.NewInt(1e8)
	eth  = big.NewInt(1e18)
	usdx = big.NewInt(1e18)
)

func bigint(val int64, decs *big.Int) *big.Int {
	return new(big.Int).Mul(big.NewInt(val), decs)
}

func TestReceive(t *testing.T) {
	chain, accts := soltest.New()

	oracleAddr, _, oracleContract, err := DeployMockOracle(accts[0].Auth, chain)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	contractAddr, _, contract, err := DeployUSDX(accts[0].Auth, chain, oracleAddr)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	t.Run("latestRoundReverts", func(t *testing.T) {
		if !chain.Succeed(oracleContract.SetAccess(accts[0].Auth, false)) {
			t.Fatal("unable to set oracle access")
		}

		accts[1].Auth.Value = big.NewInt(100)
		if chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
			t.Error("shouldn't mint when feed reverts")
		}
		accts[1].Auth.Value = nil

		if !chain.Succeed(oracleContract.SetAccess(accts[0].Auth, true)) {
			t.Fatal("unable to set oracle access")
		}
	})

	// Randomly mint usdx, ensure balances always add up.
	t.Run("mint", func(t *testing.T) {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))

		feedDecs := big.NewInt(1e8)
		expMint := func(payment *big.Int, rate int64) *big.Int {
			out := new(big.Int).Mul(payment, big.NewInt(rate))
			return out.Div(out, feedDecs)
		}

		var (
			totalPay  = new(big.Int)
			totalMint = new(big.Int)
		)

		for i := 0; i < 200; i++ {
			// Randomly select a test account.
			acct := accts[rand.Intn(len(accts))]
			// Rate is random normal variable, mean=5000usd/eth, stddev=4000usd/eth
			rate := int64(math.Abs(r.NormFloat64()*4000e8 + 5000e8))
			// Rate is random normal variable, mean=.5eth, stddev=1eth
			payment, _ := big.NewFloat(math.Abs(r.NormFloat64()*5e17 + 1e18)).Int(nil)

			preAcct, err := contract.Accounts(&bind.CallOpts{}, acct.Addr)
			if err != nil {
				t.Fatal(err)
			}

			// set rate in oracle
			if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(rate), zero, zero, zero)) {
				t.Fatalf("unable to set oracle round: rate=%v", rate)
			}

			acct.Auth.Value = payment
			if !chain.Succeed((&USDXRaw{contract}).Transfer(acct.Auth)) {
				t.Fatal("unable to transfer")
			}
			acct.Auth.Value = nil

			postAcct, err := contract.Accounts(&bind.CallOpts{}, acct.Addr)
			if err != nil {
				t.Fatal(err)
			}

			expM := expMint(payment, rate)
			if expBal := preAcct.Mint.Add(preAcct.Mint, expM); expBal.Cmp(postAcct.Mint) != 0 {
				t.Errorf("want post mint balance: %v, got: %v", expBal, postAcct.Mint)
			}

			if expLock := preAcct.Locked.Add(preAcct.Locked, payment); expLock.Cmp(postAcct.Locked) != 0 {
				t.Errorf("want post mint lock: %v, got: %v", expLock, postAcct.Locked)
			}

			totalPay.Add(totalPay, payment)
			totalMint.Add(totalMint, expM)
		}

		if totalSupply, _ := contract.TotalSupply(&bind.CallOpts{}); totalMint.Cmp(totalSupply) != 0 {
			t.Errorf("want total supply: %v, got: %v", totalMint, totalSupply)
		}

		if contractBal, _ := chain.BalanceAt(context.Background(), contractAddr, nil); totalPay.Cmp(contractBal) != 0 {
			t.Errorf("want contract balance: %v, got: %v", totalPay, contractBal)
		}
	})
}

func TestUnlock(t *testing.T) {
	chain, accts := soltest.New()

	oracleAddr, _, oracleContract, err := DeployMockOracle(accts[0].Auth, chain)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	_, _, contract, err := DeployUSDX(accts[0].Auth, chain, oracleAddr)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	// Set rate to 1000usd/eth
	if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(1000e8), zero, zero, zero)) {
		t.Fatal("unable to set oracle round")
	}

	setup := func(t *testing.T, acct soltest.TestAccount) (curBal *big.Int, cleanup func()) {
		acct.Auth.Value = big.NewInt(params.Ether)
		if !chain.Succeed((&USDXRaw{contract}).Transfer(acct.Auth)) {
			t.Fatal("unable to transfer")
		}
		acct.Auth.Value = nil

		// ensure balance is 1000usdx
		if bal, err := contract.BalanceOf(&bind.CallOpts{}, acct.Addr); err != nil {
			t.Fatal(err)
		} else if want := bigint(1000, usdx); bal.Cmp(want) != 0 {
			t.Fatalf("want bal: %v, got: %v", want, bal)
		}

		curBal, err := chain.BalanceAt(context.Background(), acct.Addr, nil)
		if err != nil {
			t.Fatal(err)
		}

		return curBal, func() {
			// redeem full balance
			if !chain.Succeed(contract.Unlock(acct.Auth, zero)) {
				t.Fatal("unable to redeem full balance")
			}
		}
	}

	// limit == 0
	t.Run("all", func(t *testing.T) {
		oldBal, _ := setup(t, accts[1])

		if !chain.Succeed(contract.Unlock(accts[1].Auth, zero)) {
			t.Fatal("unable to redeem")
		}

		if newBal, err := chain.BalanceAt(context.Background(), accts[1].Addr, nil); err != nil {
			t.Fatal(err)
		} else if want := oldBal.Add(oldBal, big.NewInt(params.Ether)); want.Cmp(newBal) != 0 {
			t.Fatalf("want new bal: %v, got: %v", want, newBal)
		}

		// Ensure no more USDX balance
		if uBal, err := contract.BalanceOf(&bind.CallOpts{}, accts[1].Addr); err != nil {
			t.Fatal(err)
		} else if uBal.Uint64() != 0 {
			t.Fatal("want 0 balance of usdx after full redeem")
		}

		if chain.Succeed(contract.Unlock(accts[1].Auth, zero)) {
			t.Fatal("redeem with 0 balance should revert")
		}
	})

	// limit < acct.mint
	t.Run("limit", func(t *testing.T) {
		oldBal, done := setup(t, accts[1])
		defer done()

		// redeem 500usdx
		if !chain.Succeed(contract.Unlock(accts[1].Auth, bigint(500, usdx))) {
			t.Fatal("unable to redeem")
		}

		if newBal, err := chain.BalanceAt(context.Background(), accts[1].Addr, nil); err != nil {
			t.Fatal(err)
		} else if want := oldBal.Add(oldBal, big.NewInt(5e17)); want.Cmp(newBal) != 0 {
			t.Fatalf("want new bal: %v, got: %v", want, newBal)
		}

		// Ensure still has 500usdx
		if uBal, err := contract.BalanceOf(&bind.CallOpts{}, accts[1].Addr); err != nil {
			t.Fatal(err)
		} else if want := bigint(500, usdx); uBal.Cmp(want) != 0 {
			t.Fatalf("want usdx balance: %v, got: %v", want, uBal)
		}
	})

	// limit > acct.mint
	t.Run("limitExceedsMint", func(t *testing.T) {
		oldBal, _ := setup(t, accts[1])

		// redeem 5000usdx
		if !chain.Succeed(contract.Unlock(accts[1].Auth, bigint(5000, usdx))) {
			t.Fatal("unable to redeem")
		}

		if newBal, err := chain.BalanceAt(context.Background(), accts[1].Addr, nil); err != nil {
			t.Fatal(err)
		} else if want := oldBal.Add(oldBal, big.NewInt(params.Ether)); want.Cmp(newBal) != 0 {
			t.Fatalf("want new bal: %v, got: %v", want, newBal)
		}

		// Ensure no more USDX balance
		if uBal, err := contract.BalanceOf(&bind.CallOpts{}, accts[1].Addr); err != nil {
			t.Fatal(err)
		} else if uBal.Uint64() != 0 {
			t.Fatal("want 0 balance of usdx after full redeem")
		}
	})

	// usdx bal < acct.mint
	t.Run("lowUSDXBal", func(t *testing.T) {
		oldBal, cleanup := setup(t, accts[1])

		// transfer 200usdx to another account
		if !chain.Succeed(contract.Transfer(accts[1].Auth, accts[2].Addr, bigint(200, usdx))) {
			t.Fatal("unable to transfer")
		}

		// redeem with no limit
		if !chain.Succeed(contract.Unlock(accts[1].Auth, zero)) {
			t.Fatal("unable to redeem")
		}

		// only get .8eth back
		if newBal, err := chain.BalanceAt(context.Background(), accts[1].Addr, nil); err != nil {
			t.Fatal(err)
		} else if want := oldBal.Add(oldBal, big.NewInt(8e17)); want.Cmp(newBal) != 0 {
			t.Fatalf("want new bal: %v, got: %v", want, newBal)
		}

		// redeeming should fail, no usdx balance
		if chain.Succeed(contract.Unlock(accts[1].Auth, zero)) {
			t.Fatal("redeeming with no usdx balance should fail")
		}

		// transfer 200usdx back, and cleanup
		if !chain.Succeed(contract.Transfer(accts[2].Auth, accts[1].Addr, bigint(200, usdx))) {
			t.Fatal("unable to transfer")
		}
		cleanup()
	})

	// usdx bal > acct.mint
	t.Run("balExceedsMint", func(t *testing.T) {
		oldBal, _ := setup(t, accts[1])
		_, cleanup := setup(t, accts[2])
		defer cleanup()

		// transfer 200usdx to accts2
		if !chain.Succeed(contract.Transfer(accts[2].Auth, accts[1].Addr, bigint(200, usdx))) {
			t.Fatal("unable to transfer")
		}

		// redeem with no limit
		if !chain.Succeed(contract.Unlock(accts[1].Auth, zero)) {
			t.Fatal("unable to redeem")
		}

		// get full eth back
		if newBal, err := chain.BalanceAt(context.Background(), accts[1].Addr, nil); err != nil {
			t.Fatal(err)
		} else if want := oldBal.Add(oldBal, big.NewInt(params.Ether)); want.Cmp(newBal) != 0 {
			t.Fatalf("want new bal: %v, got: %v", want, newBal)
		}

		// Ensure still has 200usdx
		if uBal, err := contract.BalanceOf(&bind.CallOpts{}, accts[1].Addr); err != nil {
			t.Fatal(err)
		} else if want := bigint(200, usdx); uBal.Cmp(want) != 0 {
			t.Fatalf("want usdx balance: %v, got: %v", want, uBal)
		}

		if chain.Succeed(contract.Unlock(accts[1].Auth, zero)) {
			t.Fatal("redeem with no locked eth should revert")
		}

		if !chain.Succeed(contract.Transfer(accts[1].Auth, accts[2].Addr, bigint(200, usdx))) {
			t.Fatal("unable to transfer usdx back to acct2")
		}
	})

	// mint, redeem, mint, redeem suceeds
	t.Run("doubleUnlock", func(t *testing.T) {
		_, redeem := setup(t, accts[1])
		redeem()
		_, redeem = setup(t, accts[1])
		redeem()
	})
}

func TestAppreciation(t *testing.T) {
	chain, accts := soltest.New()

	oracleAddr, _, oracleContract, err := DeployMockOracle(accts[0].Auth, chain)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	_, _, contract, err := DeployUSDX(accts[0].Auth, chain, oracleAddr)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	t.Run("doubleCollect", func(t *testing.T) {
		// rate starts at 100usd/eth
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(100e8), zero, zero, zero)) {
			t.Fatal("unable to set oracle round")
		}

		// mint 100 usdx
		accts[0].Auth.Value = big.NewInt(params.Ether)
		if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[0].Auth)) {
			t.Fatal("unable to transfer")
		}
		accts[0].Auth.Value = nil

		// rate increased to 150 usd/eth
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(150e8), zero, zero, zero)) {
			t.Fatal("unable to set oracle round")
		}

		// collect 50 usdx
		oldBal, err := contract.BalanceOf(&bind.CallOpts{}, accts[0].Addr)
		if err != nil {
			t.Fatal(err)
		}

		if !chain.Succeed(contract.CollectAppreciation(accts[0].Auth, zero)) {
			t.Fatal("unable to collect appreciation")
		}

		if newBal, err := contract.BalanceOf(&bind.CallOpts{}, accts[0].Addr); err != nil {
			t.Fatal(err)
		} else if want := oldBal.Add(oldBal, bigint(50, usdx)); newBal.Cmp(want) != 0 {
			t.Fatalf("want usdx balance: %v, got: %v", want, newBal)
		}

		// rate drops to 120, can't collect anything
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(100e8), zero, zero, zero)) {
			t.Fatal("unable to set oracle round")
		}

		if appr, err := contract.Appreciation(&bind.CallOpts{}, accts[0].Addr); err != nil {
			t.Fatal(err)
		} else if appr.Cmp(zero) != 0 {
			t.Fatalf("want appreciation: %v, got: %v", zero, appr)
		}

		// collect again @ 200
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(200e8), zero, zero, zero)) {
			t.Fatal("unable to set oracle round")
		}

		if !chain.Succeed(contract.CollectAppreciation(accts[0].Auth, zero)) {
			t.Fatal("unable to collect appreciation")
		}

		if newBal, err := contract.BalanceOf(&bind.CallOpts{}, accts[0].Addr); err != nil {
			t.Fatal(err)
		} else if want := oldBal.Add(oldBal, bigint(50, usdx)); newBal.Cmp(want) != 0 {
			t.Fatalf("want usdx balance: %v, got: %v", want, newBal)
		}

		// rate increases to 250, can collect 50
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(250e8), zero, zero, zero)) {
			t.Fatal("unable to set oracle round")
		}
		if appr, err := contract.Appreciation(&bind.CallOpts{}, accts[0].Addr); err != nil {
			t.Fatal(err)
		} else if want := bigint(50, usdx); appr.Cmp(want) != 0 {
			t.Fatalf("want appreciation: %v, got: %v", want, appr)
		}

		// rate increases to 300, can collect 100
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(300e8), zero, zero, zero)) {
			t.Fatal("unable to set oracle round")
		}
		if appr, err := contract.Appreciation(&bind.CallOpts{}, accts[0].Addr); err != nil {
			t.Fatal(err)
		} else if want := bigint(100, usdx); appr.Cmp(want) != 0 {
			t.Fatalf("want appreciation: %v, got: %v", want, appr)
		}

		// redeem everything to reset
		if !chain.Succeed(contract.Unlock(accts[0].Auth, zero)) {
			t.Fatal("unable to redeem")
		}
	})

	t.Run("limitOverAppr", func(t *testing.T) {
		// rate starts at 100usd/eth
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(100e8), zero, zero, zero)) {
			t.Fatal("unable to set oracle round")
		}

		// mint 100 usdx
		accts[0].Auth.Value = big.NewInt(params.Ether)
		if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[0].Auth)) {
			t.Fatal("unable to transfer")
		}
		accts[0].Auth.Value = nil

		// rate increased to 150 usd/eth
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(150e8), zero, zero, zero)) {
			t.Fatal("unable to set oracle round")
		}

		oldBal, err := contract.BalanceOf(&bind.CallOpts{}, accts[0].Addr)
		if err != nil {
			t.Fatal(err)
		}

		if !chain.Succeed(contract.CollectAppreciation(accts[0].Auth, bigint(100, usdx))) {
			t.Fatal("unable to collect appreciation")
		}

		if newBal, err := contract.BalanceOf(&bind.CallOpts{}, accts[0].Addr); err != nil {
			t.Fatal(err)
		} else if want := oldBal.Add(oldBal, bigint(50, usdx)); newBal.Cmp(want) != 0 {
			t.Fatalf("want usdx balance: %v, got: %v", want, newBal)
		}

		// redeem everything to reset
		if !chain.Succeed(contract.Unlock(accts[0].Auth, zero)) {
			t.Fatal("unable to redeem")
		}
	})

	t.Run("limitUnderAppr", func(t *testing.T) {
		// rate starts at 100usd/eth
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(100e8), zero, zero, zero)) {
			t.Fatal("unable to set oracle round")
		}

		// mint 100 usdx
		accts[0].Auth.Value = big.NewInt(params.Ether)
		if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[0].Auth)) {
			t.Fatal("unable to transfer")
		}
		accts[0].Auth.Value = nil

		// rate increased to 150 usd/eth
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(150e8), zero, zero, zero)) {
			t.Fatal("unable to set oracle round")
		}

		oldBal, err := contract.BalanceOf(&bind.CallOpts{}, accts[0].Addr)
		if err != nil {
			t.Fatal(err)
		}

		if !chain.Succeed(contract.CollectAppreciation(accts[0].Auth, bigint(25, usdx))) {
			t.Fatal("unable to collect appreciation")
		}

		if newBal, err := contract.BalanceOf(&bind.CallOpts{}, accts[0].Addr); err != nil {
			t.Fatal(err)
		} else if want := oldBal.Add(oldBal, bigint(25, usdx)); newBal.Cmp(want) != 0 {
			t.Fatalf("want usdx balance: %v, got: %v", want, newBal)
		}

		// redeem everything to reset
		if !chain.Succeed(contract.Unlock(accts[0].Auth, zero)) {
			t.Fatal("unable to redeem")
		}
	})

	tests := []struct {
		prices  []int64
		expAppr int64
	}{
		{[]int64{100}, 0},
		{[]int64{100, 90}, 0},
		{[]int64{100, 110}, 10},
		{[]int64{100, 90, 150}, 110},
		{[]int64{100, 110, 150}, 90},
		{[]int64{100, 90, 50}, 0},
		{[]int64{100, 110, 90, 150}, 150},
		// The following case is interesting.  If the 100 and 150
		// deposits were done via 2 separate accounts, the 100 account
		// deposit would be able to collect 20usdx of appreciation if
		// price rises to 120, and the 150 deposit would be 30usdx
		// underwater. Combining the 2 transactions into a single
		// transaction averages the basis, such that 2 eth have been
		// deposited at an average cost of 125usd/eth.  This might not
		// be a tradeoff the depositor is willing to make, but the
		// mitigation against this would add too much complexity.
		{[]int64{100, 150, 120}, 0},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			// Mint 1 eth at each price
			for _, price := range test.prices {
				// set oracle to the price
				if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, bigint(price, rate), zero, zero, zero)) {
					t.Fatal("unable to set oracle round")
				}

				accts[0].Auth.Value = big.NewInt(params.Ether)
				// mint 1 eth
				if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[0].Auth)) {
					t.Fatal("unable to transfer")
				}
				accts[0].Auth.Value = nil
			}

			// call appreciation, ensure it's expected
			appr, err := contract.Appreciation(&bind.CallOpts{}, accts[0].Addr)
			if err != nil {
				t.Fatal(err)
			} else if want := bigint(test.expAppr, usdx); appr.Cmp(want) != 0 {
				t.Fatalf("want appreciation: %v, got: %v", want, appr)
			}

			oldBal, err := contract.BalanceOf(&bind.CallOpts{}, accts[0].Addr)
			if err != nil {
				t.Fatal(err)
			}

			if !chain.Succeed(contract.CollectAppreciation(accts[0].Auth, zero)) {
				t.Fatal("unable to collect appreciation")
			}

			if newBal, err := contract.BalanceOf(&bind.CallOpts{}, accts[0].Addr); err != nil {
				t.Fatal(err)
			} else if want := oldBal.Add(oldBal, bigint(test.expAppr, usdx)); newBal.Cmp(want) != 0 {
				t.Fatalf("want usdx balance: %v, got: %v", want, newBal)
			}

			// redeem everything to reset
			if !chain.Succeed(contract.Unlock(accts[0].Auth, zero)) {
				t.Fatal("unable to redeem")
			}
		})
	}
}

func TestSetFeed(t *testing.T) {
	chain, accts := soltest.New()

	oracleAddr, _, _, err := DeployMockOracle(accts[0].Auth, chain)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	_, _, contract, err := DeployUSDX(accts[0].Auth, chain, oracleAddr)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	t.Run("constructorSetsPriceFeed", func(t *testing.T) {
		priceFeed, err := contract.UsdPriceFeed(&bind.CallOpts{})
		if err != nil {
			t.Errorf("uexpected err reading ethUsdPriceFeed: %v", err)
		}
		if priceFeed != oracleAddr {
			t.Errorf("want ethUsdPriceFeed: %v, got: %v", oracleAddr, priceFeed)
		}
	})

	t.Run("feedDecimalsIncorrect", func(t *testing.T) {
		if chain.Succeed(contract.SetFeed(accts[0].Auth, common.Address{0xff})) {
			t.Error("owner shouldn't set invalid feed")
		}
	})

	t.Run("ownerReplacesFeed", func(t *testing.T) {
		newOracle, _, _, err := DeployMockOracle(accts[0].Auth, chain)
		if err != nil {
			t.Fatal(err)
		}
		chain.Commit()

		if chain.Succeed(contract.SetFeed(accts[1].Auth, newOracle)) {
			t.Fatal("non-owner shouldn't replace feed")
		}

		// owner can change
		if !chain.Succeed(contract.SetFeed(accts[0].Auth, newOracle)) {
			t.Error("owner should setFeed")
		}

		if feed, err := contract.UsdPriceFeed(&bind.CallOpts{}); err != nil {
			t.Fatal("unable to get feed")
		} else if feed != newOracle {
			t.Errorf("want feed: %s, got: %s", newOracle, feed)
		}

		// owner changes feed back
		if !chain.Succeed(contract.SetFeed(accts[0].Auth, oracleAddr)) {
			t.Fatal("owner should setFeed")
		}
	})
}

func TestThreshold(t *testing.T) {
	chain, accts := soltest.New()

	oracleAddr, _, oracleContract, err := DeployMockOracle(accts[0].Auth, chain)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	_, _, contract, err := DeployUSDX(accts[0].Auth, chain, oracleAddr)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	tt := big.NewInt(3)
	t.Run("ownerSets", func(t *testing.T) {
		if chain.Succeed(contract.SetStalenessThreshold(accts[1].Auth, tt)) {
			t.Fatal("non-owner shouldn't set threshold")
		}

		if !chain.Succeed(contract.SetStalenessThreshold(accts[0].Auth, tt)) {
			t.Fatal("non-owner shouldn't set threshold")
		}
	})

	t.Run("receive", func(t *testing.T) {
		if !chain.Succeed(contract.SetStalenessThreshold(accts[0].Auth, tt)) {
			t.Fatal("non-owner shouldn't set threshold")
		}

		// set oracle to stale
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, big.NewInt(14), bigint(1000, rate), zero, zero, big.NewInt(10))) {
			t.Fatalf("unable to set oracle round: rate=%v", rate)
		}

		// sending eth should fail when oracle is stale
		accts[1].Auth.Value = big.NewInt(params.Ether)
		if chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
			t.Fatal("transfer should fail when oracle is stale")
		}

		// set oracle to fresh
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, big.NewInt(14), bigint(1000, rate), zero, zero, big.NewInt(11))) {
			t.Fatalf("unable to set oracle round: rate=%v", rate)
		}

		// sending eth should succeed when oracle is fresh
		if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
			t.Fatal("transfer should succeed when oracle is fresh")
		}

		accts[1].Auth.Value = nil
	})

	t.Run("appreciation", func(t *testing.T) {
		if !chain.Succeed(contract.SetStalenessThreshold(accts[0].Auth, tt)) {
			t.Fatal("non-owner shouldn't set threshold")
		}

		// bootstrap acct
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, bigint(1000, rate), zero, zero, zero)) {
			t.Fatalf("unable to set oracle round: rate=%v", rate)
		}

		// sending eth should fail when oracle is stale
		accts[2].Auth.Value = big.NewInt(params.Ether)
		if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[2].Auth)) {
			t.Fatal("unable to mint usdx")
		}
		accts[2].Auth.Value = nil

		// set oracle to appreciate, but stale
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, big.NewInt(14), bigint(2000, rate), zero, zero, big.NewInt(10))) {
			t.Fatalf("unable to set oracle round: rate=%v", rate)
		}

		// collectingAppreciation should fail when oracle is stale
		if chain.Succeed(contract.CollectAppreciation(accts[2].Auth, zero)) {
			t.Fatal("shouldn't collect appreciation on stale oracle")
		}

		// set oracle to fresh
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, big.NewInt(14), bigint(2000, rate), zero, zero, big.NewInt(11))) {
			t.Fatalf("unable to set oracle round: rate=%v", rate)
		}

		// collectingAppreciation should succeed when oracle is fresh
		if !chain.Succeed(contract.CollectAppreciation(accts[2].Auth, zero)) {
			t.Fatal("collectAppreciation should succeed with fresh oracle")
		}
	})
}

func TestTransferAcct(t *testing.T) {
	chain, accts := soltest.New()

	oracleAddr, _, oracleContract, err := DeployMockOracle(accts[0].Auth, chain)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	_, _, contract, err := DeployUSDX(accts[0].Auth, chain, oracleAddr)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	// set 1000usd/eth rate in oracle
	mint := bigint(1000, rate)
	if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, mint, zero, zero, zero)) {
		t.Fatalf("unable to set oracle round: rate=%v", rate)
	}

	t.Run("transferToExisting", func(t *testing.T) {
		// acct1 mints 1 eth
		accts[1].Auth.Value = bigint(1, eth)
		if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
			t.Fatal("unable to transfer")
		}
		accts[1].Auth.Value = nil

		// acct2 mints 1 eth
		accts[2].Auth.Value = bigint(1, eth)
		if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[2].Auth)) {
			t.Fatal("unable to transfer")
		}
		accts[2].Auth.Value = nil

		// acct1 can't send to acct2
		if chain.Succeed(contract.TransferAcct(accts[1].Auth, accts[2].Addr)) {
			t.Fatal("shouldn't transfer to account with existing balance")
		}

		// acct1 redeems
		if !chain.Succeed(contract.Unlock(accts[1].Auth, zero)) {
			t.Fatal("redeem failed")
		}

		// acct2 redeems
		if !chain.Succeed(contract.Unlock(accts[2].Auth, zero)) {
			t.Fatal("redeem failed")
		}
	})

	t.Run("success", func(t *testing.T) {
		// acct1 mints 1 eth
		accts[1].Auth.Value = bigint(1, eth)
		if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
			t.Fatal("unable to transfer")
		}
		accts[1].Auth.Value = nil

		// transfer account
		if !chain.Succeed(contract.TransferAcct(accts[1].Auth, accts[2].Addr)) {
			t.Fatal("account transfer failed")
		}

		// accounts have successfully changed
		if acct, err := contract.Accounts(&bind.CallOpts{}, accts[1].Addr); err != nil {
			t.Fatal("unable to view account")
		} else if acct.Locked.Cmp(zero) != 0 {
			t.Fatalf("want acct locked: %v, got: %v", zero, acct.Locked)
		} else if acct.Mint.Cmp(zero) != 0 {
			t.Fatalf("want acct mint: %v, got: %v", zero, acct.Mint)
		}

		if acct, err := contract.Accounts(&bind.CallOpts{}, accts[2].Addr); err != nil {
			t.Fatal("unable to view account")
		} else if wantLock := bigint(1, eth); acct.Locked.Cmp(wantLock) != 0 {
			t.Fatalf("want acct locked: %v, got: %v", wantLock, acct.Locked)
		} else if wantMint := bigint(1000, usdx); acct.Mint.Cmp(wantMint) != 0 {
			t.Fatalf("want acct mint: %v, got: %v", wantMint, acct.Mint)
		}

		// usdx balance doesn't change
	})
}

func TestOwner(t *testing.T) {
	chain, accts := soltest.New()

	oracleAddr, _, _, err := DeployMockOracle(accts[0].Auth, chain)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	_, _, contract, err := DeployUSDX(accts[0].Auth, chain, oracleAddr)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	t.Run("constructorSetsOwner", func(t *testing.T) {
		owner, err := contract.Owner(&bind.CallOpts{})
		if err != nil {
			t.Errorf("uexpected err reading owner: %v", err)
		}
		if owner != accts[0].Addr {
			t.Errorf("want owner: %v, got: %v", accts[0].Addr, owner)
		}
	})

	t.Run("ownerChangesOwner", func(t *testing.T) {
		// acct1 can't change owner
		if chain.Succeed(contract.TransferOwnership(accts[1].Auth, accts[2].Addr)) {
			t.Error("non-owner can TransferOwnership")
		}

		// acct0 can change owner to acct1
		if !chain.Succeed(contract.TransferOwnership(accts[0].Auth, accts[1].Addr)) {
			t.Error("owner can't TransferOwnership")
		}

		if newOwner, err := contract.Owner(&bind.CallOpts{}); err != nil {
			t.Errorf("uexpected err reading owner: %v", err)
		} else if newOwner != accts[1].Addr {
			t.Errorf("want owner: %v, got: %v", accts[1].Addr, newOwner)
		}

		// acct0 can no longer TransferOwnership
		if chain.Succeed(contract.TransferOwnership(accts[0].Auth, accts[1].Addr)) {
			t.Error("non-owner can TransferOwnership")
		}

		// acct1 can TransferOwnership to acct0
		if !chain.Succeed(contract.TransferOwnership(accts[1].Auth, accts[0].Addr)) {
			t.Error("owner can't TransferOwnership")
		}

		if newOwner, err := contract.Owner(&bind.CallOpts{}); err != nil {
			t.Errorf("uexpected err reading owner: %v", err)
		} else if newOwner != accts[0].Addr {
			t.Errorf("want owner: %v, got: %v", accts[0].Addr, newOwner)
		}
	})
}

func TestERC20(t *testing.T) {
	chain, accts := soltest.New()

	oracleAddr, _, oracleContract, err := DeployMockOracle(accts[0].Auth, chain)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	_, _, contract, err := DeployUSDX(accts[0].Auth, chain, oracleAddr)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	txEvts := make(chan *USDXTransfer, 1)
	txSub, err := contract.USDXFilterer.WatchTransfer(nil, txEvts, nil, nil)
	if err != nil {
		t.Fatal("unable to watch for transfer events")
	}
	defer txSub.Unsubscribe()
	checkTxEvt := func(expFrom, expTo common.Address, expVal *big.Int) {
		select {
		case tx := <-txEvts:
			if bytes.Compare(tx.From.Bytes(), expFrom.Bytes()) != 0 {
				t.Fatalf("want mint evt from: %v, got: %v", expFrom, tx.From)
			} else if bytes.Compare(tx.To.Bytes(), expTo.Bytes()) != 0 {
				t.Fatalf("want mint evt to: %v, got: %v", expTo, tx.To)
			} else if tx.Value.Cmp(expVal) != 0 {
				t.Fatalf("want mint evt val: %v, got: %v", expVal, tx.Value)
			}
		case <-time.After(time.Second):
			t.Fatal("expected transfer event")
		}
	}

	// set 1000usd/eth rate in oracle
	mint := bigint(1000, rate)
	if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, mint, zero, zero, zero)) {
		t.Fatalf("unable to set oracle round: rate=%v", rate)
	}

	// accts1 sends 1 eth to get 1000 usdx
	accts[1].Auth.Value = bigint(1, eth)
	if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
		t.Fatal("unable to transfer")
	}
	accts[1].Auth.Value = nil
	checkTxEvt(common.Address{}, accts[1].Addr, bigint(1000, usdx))

	if dec, err := contract.Decimals(&bind.CallOpts{}); err != nil {
		t.Fatal("unable to get usdx decimals")
	} else if dec != 18 {
		t.Fatalf("want decs: %d, got: %d", 18, dec)
	}

	startBal := bigint(1000, usdx)
	if bal, err := contract.BalanceOf(&bind.CallOpts{}, accts[1].Addr); err != nil {
		t.Fatal("err finding acct1 balance:", err)
	} else if bal.Cmp(startBal) != 0 {
		t.Errorf("want acct1 initial bal: %v, got: %v", startBal, bal)
	}

	// Ensure accts2 has 0 balance.
	if bal, err := contract.BalanceOf(&bind.CallOpts{}, accts[2].Addr); err != nil {
		t.Fatal("err finding acct2 balance:", err)
	} else if bal.Uint64() != 0 {
		t.Errorf("want acct2 initial bal: %v, got: %v", 0, bal)
	}

	// Transfer 600 usdx
	txnAmount := bigint(600, usdx)
	if !chain.Succeed(contract.Transfer(accts[1].Auth, accts[2].Addr, txnAmount)) {
		t.Fatal("unable to transfer")
	}
	checkTxEvt(accts[1].Addr, accts[2].Addr, txnAmount)

	// Recheck the balances.
	// Ensure acct1 balance has decreased.
	if bal, err := contract.BalanceOf(&bind.CallOpts{}, accts[1].Addr); err != nil {
		t.Fatal("after tx, err finding acct1 balance:", err)
	} else if expBal := new(big.Int).Sub(startBal, txnAmount); bal.Cmp(expBal) != 0 {
		t.Errorf("after tx, want acct1 bal: %v, got: %v", expBal, bal)
	}

	// Ensure acct2 balance contains transferred amount.
	if bal, err := contract.BalanceOf(&bind.CallOpts{}, accts[2].Addr); err != nil {
		t.Fatal("after tx, err finding balance:", err)
	} else if bal.Cmp(txnAmount) != 0 {
		t.Fatalf("after tx, want acct2 bal: %v, got: %v", txnAmount, bal)
	}

	// acct2 sends back
	if !chain.Succeed(contract.Transfer(accts[2].Auth, accts[1].Addr, txnAmount)) {
		t.Fatal("unable to transfer")
	}
	checkTxEvt(accts[2].Addr, accts[1].Addr, txnAmount)

	// acct1 redeems
	if !chain.Succeed(contract.Unlock(accts[1].Auth, zero)) {
		t.Fatal("unable to redeem")
	}

	// check burn event
	checkTxEvt(accts[1].Addr, common.Address{}, bigint(1000, usdx))
}
