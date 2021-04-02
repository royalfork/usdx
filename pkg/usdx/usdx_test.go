//go:generate abigen --sol ../../sol/Usdx.sol --pkg usdx --out usdx_abigen.go --exc ../../sol/chainlink/evm-contracts/src/v0.7/interfaces/AggregatorV3Interface.sol:AggregatorV3Interface
package usdx

import (
	"context"
	"math"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/params"
	"github.com/royalfork/soltest"
)

var zero = new(big.Int)

func TestSetFeed(t *testing.T) {
	// TODO:
	// ensure only owner
	// ensure revert if decimals don't match up
}

func TestTransferAcct(t *testing.T) {
	// TODO:
	// ensure only acct owner can transfer
}

func TestReceive(t *testing.T) {
	chain, accts := soltest.New()

	oracleAddr, _, oracleContract, err := DeployMockOracle(accts[0].Auth, chain)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	if !chain.Succeed(oracleContract.SetDecimals(accts[0].Auth, 8)) {
		t.Fatal("unable to set oracle decs")
	}

	contractAddr, _, contract, err := DeployUSDX(accts[0].Auth, chain, oracleAddr)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	// TODO: Listen for mint events

	t.Run("latestRoundReverts", func(t *testing.T) {
		if !chain.Succeed(oracleContract.SetAccess(accts[0].Auth, false)) {
			t.Fatal("unable to set oracle access")
		}

		accts[1].Auth.Value = big.NewInt(100)
		if chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
			t.Error("shouldn't mint when feed reverts")
		}
	})

	// Randomly mint usdx, ensure balances always add up.
	t.Run("mint", func(t *testing.T) {
		// rand.Seed(time.Now().UnixNano())
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

	// is it possible for the division in weiToUSDX to truncate? What are implications of this?
	// TODO:
	t.Run("fractionalMint", func(t *testing.T) {
	})
}

func TestRedeem(t *testing.T) {
	// deploy
	chain, accts := soltest.New()

	oracleAddr, _, oracleContract, err := DeployMockOracle(accts[0].Auth, chain)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	if !chain.Succeed(oracleContract.SetDecimals(accts[0].Auth, 8)) {
		t.Fatal("unable to set oracle decs")
	}

	_, _, contract, err := DeployUSDX(accts[0].Auth, chain, oracleAddr)
	if err != nil {
		t.Fatal(err)
	}
	chain.Commit()

	t.Run("insufficientUSDXBalance", func(t *testing.T) {
	})

	t.Run("limitExceedsBalance", func(t *testing.T) {
	})

	t.Run("limit", func(t *testing.T) {
	})

	t.Run("all", func(t *testing.T) {
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(1000e8), zero, zero, zero)) {
			t.Fatal("unable to set oracle round")
		}
		accts[1].Auth.Value = big.NewInt(params.Ether)
		if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
			t.Fatal("unable to transfer")
		}
		accts[1].Auth.Value = nil

		// ensure balance is 1000usdx
		if bal, err := contract.BalanceOf(&bind.CallOpts{}, accts[1].Addr); err != nil {
			t.Fatal(err)
		} else if want := new(big.Int).Mul(big.NewInt(1000), big.NewInt(1e18)); bal.Cmp(want) != 0 {
			t.Fatalf("want bal: %v, got: %v", want, bal)
		}

		oldBal, err := chain.BalanceAt(context.Background(), accts[1].Addr, nil)
		if err != nil {
			t.Fatal(err)
		}

		if !chain.Succeed(contract.Redeem(accts[1].Auth, zero)) {
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

	t.Run("balExceedsMint", func(t *testing.T) {
		// if user's usdx balance is more than the amount they mint, redemption should leave balance difference
	})

	// TODO:
	// ensure partial redemption works (what happens to "receive" after redemption?)
	// what happens to fractional remainder?
}

func TestAppreciation(t *testing.T) {
	// TODO:
	// test various permutations of "receive" price going up/down
	// both appreciation and collectAppreciation is calculated properly
	// collectAppreciation called twice with same rate returns 0 2nd time
	// negative appreciation can't be collected
	// collect limit is respected
}

func TestOwner(t *testing.T) {
	// TODO:
	// basic owner/transfer owner works
}

func TestERC20(t *testing.T) {
	// TODO:
	// basic transfer and balance methods work
	// event is sent for both mint and burn
}

// func TestAllowableMint(t *testing.T) {
// 	// deploy everything
// 	chain, accts := soltest.New()

// 	oracleAddr, _, oracleContract, err := DeployMockOracle(accts[0].Auth, chain)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	chain.Commit()

// 	if !chain.Succeed(oracleContract.SetDecimals(accts[0].Auth, 8)) {
// 		t.Fatal("unable to set oracle decs")
// 	}

// 	_, _, contract, err := DeployUSDX(accts[0].Auth, chain, oracleAddr)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	chain.Commit()

// 	// set oracle to 100usd/eth
// 	zero := big.NewInt(0)
// 	if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(1e10), zero, zero, zero)) {
// 		t.Fatal("unable to set oracle round")
// 	}

// 	// Locking 1 eth should allow mint of 100usdx
// 	if allowed, err := contract.AllowableMint(&bind.CallOpts{}, big.NewInt(params.Ether)); err != nil {
// 		t.Fatal(err)
// 	} else if want := new(big.Int).Mul(big.NewInt(100), big.NewInt(params.Ether)); allowed.Cmp(want) != 0 {
// 		t.Errorf("want allowable mint: %v, got: %v", want, allowed)
// 	}

// 	// implement mint
// 	// - send payment
// 	// - gets sent into account
// 	accts[1].Auth.Value = big.NewInt(params.Ether)
// 	if !chain.Succeed(contract.Mint(accts[1].Auth)) {
// 		t.Fatal("unable to mint")
// 	}

// 	bal, err := contract.BalanceOf(&bind.CallOpts{}, accts[1].Addr)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	fmt.Printf("bal = %+v\n", bal) // output for debug

// 	// if rate goes up to 110usd/eth, allowed to mint 10 usdx more
// 	if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(11e9), zero, zero, zero)) {
// 		t.Fatal("unable to set oracle round")
// 	}

// 	mint, err := contract.AllowableMint(&bind.CallOpts{From: accts[1].Addr}, big.NewInt(0))
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	fmt.Printf("mint = %+v\n", mint) // output for debug

// 	// if rate goes down to 90usd/eth, can't mint anymore
// 	if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(9e9), zero, zero, zero)) {
// 		t.Fatal("unable to set oracle round")
// 	}

// 	mint, err = contract.AllowableMint(&bind.CallOpts{From: accts[1].Addr}, big.NewInt(0))
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	fmt.Printf("mint = %+v\n", mint) // output for debug

// 	mint, err = contract.AllowableMint(&bind.CallOpts{From: accts[1].Addr}, big.NewInt(params.Ether))
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	fmt.Printf("mint = %+v\n", mint) // output for debug

// 	// contract.AllowableMint(accts[1].Auth, _payment*big.Int)
// 	// if rate stays same/goes down, no more can be minted
// }

// func TestUsdx(t *testing.T) {
// 	chain, accts := soltest.New()

// 	oracleAddr, _, oracleContract, err := DeployMockOracle(accts[0].Auth, chain)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	chain.Commit()

// 	_, _, contract, err := DeployUSDX(accts[0].Auth, chain, oracleAddr)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	chain.Commit()

// 	t.Run("constructorSetsPriceFeed", func(t *testing.T) {
// 		priceFeed, err := contract.EthUsdPriceFeed(&bind.CallOpts{})
// 		if err != nil {
// 			t.Errorf("uexpected err reading ethUsdPriceFeed: %v", err)
// 		}
// 		if priceFeed != oracleAddr {
// 			t.Errorf("want ethUsdPriceFeed: %v, got: %v", oracleAddr, priceFeed)
// 		}
// 	})

// 	t.Run("constructorSetsOwner", func(t *testing.T) {
// 		owner, err := contract.Owner(&bind.CallOpts{})
// 		if err != nil {
// 			t.Errorf("uexpected err reading owner: %v", err)
// 		}
// 		if owner != accts[0].Addr {
// 			t.Errorf("want owner: %v, got: %v", accts[0].Addr, owner)
// 		}
// 	})

// 	t.Run("ownerChangesOwner", func(t *testing.T) {
// 		// acct1 can't change owner
// 		if chain.Succeed(contract.TransferOwnership(accts[1].Auth, accts[2].Addr)) {
// 			t.Error("non-owner can TransferOwnership")
// 		}

// 		// acct0 can change owner to acct1
// 		if !chain.Succeed(contract.TransferOwnership(accts[0].Auth, accts[1].Addr)) {
// 			t.Error("owner can't TransferOwnership")
// 		}

// 		if newOwner, err := contract.Owner(&bind.CallOpts{}); err != nil {
// 			t.Errorf("uexpected err reading owner: %v", err)
// 		} else if newOwner != accts[1].Addr {
// 			t.Errorf("want owner: %v, got: %v", accts[1].Addr, newOwner)
// 		}

// 		// acct0 can no longer TransferOwnership
// 		if chain.Succeed(contract.TransferOwnership(accts[0].Auth, accts[1].Addr)) {
// 			t.Error("non-owner can TransferOwnership")
// 		}

// 		// acct1 can TransferOwnership to acct0
// 		if !chain.Succeed(contract.TransferOwnership(accts[1].Auth, accts[0].Addr)) {
// 			t.Error("owner can't TransferOwnership")
// 		}

// 		if newOwner, err := contract.Owner(&bind.CallOpts{}); err != nil {
// 			t.Errorf("uexpected err reading owner: %v", err)
// 		} else if newOwner != accts[0].Addr {
// 			t.Errorf("want owner: %v, got: %v", accts[0].Addr, newOwner)
// 		}
// 	})

// 	t.Run("ownerReplacesFeed", func(t *testing.T) {
// 		newOracle := common.Address{0xff}

// 		if chain.Succeed(contract.SetFeed(accts[1].Auth, newOracle)) {
// 			t.Fatal("non-owner shouldn't replace feed")
// 		}

// 		// owner can change
// 		if !chain.Succeed(contract.SetFeed(accts[0].Auth, newOracle)) {
// 			t.Error("owner should setFeed")
// 		}

// 		if feed, err := contract.EthUsdPriceFeed(&bind.CallOpts{}); err != nil {
// 			t.Fatal("unable to get feed")
// 		} else if feed != newOracle {
// 			t.Errorf("want feed: %s, got: %s", newOracle, feed)
// 		}

// 		// owner changes feed back
// 		if !chain.Succeed(contract.SetFeed(accts[0].Auth, oracleAddr)) {
// 			t.Fatal("owner should setFeed")
// 		}
// 	})

// 	t.Run("feedDecimalsTooLarge", func(t *testing.T) {
// 		if !chain.Succeed(oracleContract.SetDecimals(accts[0].Auth, 65)) {
// 			t.Fatalf("unable to set oracle decs: decs=%v", 65)
// 		}

// 		accts[1].Auth.Value = big.NewInt(100)
// 		if chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
// 			t.Error("shouldn't mint when decs is too large")
// 		}

// 		if !chain.Succeed(oracleContract.SetDecimals(accts[0].Auth, 64)) {
// 			t.Fatalf("unable to set oracle decs: decs=%v", 64)
// 		}

// 		if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
// 			t.Error("should mint when decs is 64")
// 		}
// 	})

// 	t.Run("feedRevertsOnMint", func(t *testing.T) {
// 		if !chain.Succeed(oracleContract.SetAccess(accts[0].Auth, false)) {
// 			t.Fatal("unable to set oracle access")
// 		}

// 		accts[1].Auth.Value = big.NewInt(100)
// 		if chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
// 			t.Error("shouldn't mint when feed reverts")
// 		}

// 		if !chain.Succeed(oracleContract.SetAccess(accts[0].Auth, true)) {
// 			t.Fatal("unable to set oracle access")
// 		}

// 		if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
// 			t.Error("should mint when feed doesn't revert")
// 		}
// 	})

// 	t.Run("mintOnReceive", func(t *testing.T) {
// 		var payments = []*big.Int{
// 			big.NewInt(params.Wei),
// 			new(big.Int).Mul(big.NewInt(10), big.NewInt(params.Wei)),
// 			new(big.Int).Mul(big.NewInt(200), big.NewInt(params.Wei)),
// 			new(big.Int).Mul(big.NewInt(3000), big.NewInt(params.Wei)),
// 			big.NewInt(params.GWei),
// 			new(big.Int).Mul(big.NewInt(10), big.NewInt(params.GWei)),
// 			new(big.Int).Mul(big.NewInt(200), big.NewInt(params.GWei)),
// 			new(big.Int).Mul(big.NewInt(3000), big.NewInt(params.GWei)),
// 			big.NewInt(params.Ether),
// 			new(big.Int).Mul(big.NewInt(10), big.NewInt(params.Ether)),
// 			new(big.Int).Mul(big.NewInt(200), big.NewInt(params.Ether)),
// 			new(big.Int).Mul(big.NewInt(3000), big.NewInt(params.Ether)),
// 		}

// 		var rates = []int64{
// 			1,
// 			2e6,
// 			1e8, // 1 eth = 1 usd, at 8 decimals
// 			3e10,
// 			1e18,
// 			58973604819,
// 			589736048190,
// 			58973604819000,
// 		}

// 		var rateDecs = []uint8{
// 			0,
// 			1,
// 			6,
// 			8, // normal
// 			12,
// 			18,
// 		}

// 		txEvts := make(chan *USDXTransfer, 1)
// 		txSub, err := contract.USDXFilterer.WatchTransfer(nil, txEvts, nil, nil)
// 		if err != nil {
// 			t.Fatal("unable to watch for transfer events")
// 		}
// 		defer txSub.Unsubscribe()

// 		// returns receiving address and amount of last transfer operation.
// 		transferEvt := func() (to common.Address, value *big.Int, err error) {
// 			select {
// 			case e := <-txEvts:
// 				return e.To, e.Value, nil

// 			case err := <-txSub.Err():
// 				return common.Address{}, nil, err

// 			case <-time.After(time.Second):
// 				return common.Address{}, nil, errors.New("timeout")
// 			}
// 		}

// 		expAmount := func(rate int64, rateDec uint8, payment *big.Int) *big.Int {
// 			// TODO refactor this so it's actually readable
// 			res := new(big.Int).Mul(big.NewInt(rate), payment)
// 			return res.Div(res, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(rateDec)), nil))
// 		}

// 		zero := new(big.Int)
// 		for _, rate := range rates {
// 			for _, rateDec := range rateDecs {
// 				for _, payment := range payments {
// 					// set rate in oracle
// 					if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(rate), zero, zero, zero)) {
// 						t.Fatalf("unable to set oracle round: rate=%v", rate)
// 					}

// 					// set rateDec in oracle
// 					if !chain.Succeed(oracleContract.SetDecimals(accts[0].Auth, rateDec)) {
// 						t.Fatalf("unable to set oracle decs: decs=%v", rateDec)
// 					}

// 					accts[1].Auth.Value = payment
// 					if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
// 						t.Fatal("unable to transfer")
// 					}

// 					to, amt, err := transferEvt()
// 					if err != nil {
// 						t.Error(err)
// 					}
// 					if to != accts[1].Addr {
// 						t.Errorf("want transfer addr: %v, got: %v", accts[1], to)
// 					}
// 					if expAmt := expAmount(rate, rateDec, payment); amt.Cmp(expAmt) != 0 {
// 						t.Errorf("rate=%d, rateDec=%d, payment=%v. want amount: %v, got: %v", rate, rateDec, payment, expAmt, amt)
// 					}
// 				}
// 			}
// 		}
// 	})

// 	t.Run("erc20Transfer", func(t *testing.T) {
// 		var (
// 			to, from = accts[2], accts[3]
// 			dec      = uint8(6)                                                   // rate contains 6 decimals
// 			rate     = big.NewInt(1_000_000_000)                                  // 1,000 usd = 1 eth
// 			send     = new(big.Int).Mul(big.NewInt(10), big.NewInt(params.Ether)) // send 10 eth
// 			expBal   = big.NewInt(10_000)                                         // expect 10,000 usdx

// 			zero = new(big.Int)
// 		)

// 		// set rate in oracle
// 		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, rate, zero, zero, zero)) {
// 			t.Fatalf("unable to set oracle round: rate=%v", rate)
// 		}

// 		if !chain.Succeed(oracleContract.SetDecimals(accts[0].Auth, dec)) {
// 			t.Fatalf("unable to set oracle decs: decs=%v", dec)
// 		}

// 		to.Auth.Value = send
// 		if !chain.Succeed((&USDXRaw{contract}).Transfer(to.Auth)) {
// 			t.Fatal("unable to transfer")
// 		}

// 		// Ensure contract creator has expected balance.
// 		usdxDec, err := contract.Decimals(&bind.CallOpts{})
// 		if err != nil {
// 			t.Fatal("unable to get usdx decimals")
// 		}

// 		startBal := new(big.Int).Mul(expBal, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(usdxDec)), nil))
// 		if bal, err := contract.BalanceOf(&bind.CallOpts{}, to.Addr); err != nil {
// 			t.Fatal("err finding acct1 balance:", err)
// 		} else if bal.Cmp(startBal) != 0 {
// 			t.Errorf("want acct1 initial bal: %v, got: %v", startBal, bal)
// 		}

// 		// Ensure other user has 0 balance.
// 		if bal, err := contract.BalanceOf(&bind.CallOpts{}, from.Addr); err != nil {
// 			t.Fatal("err finding acct2 balance:", err)
// 		} else if bal.Uint64() != 0 {
// 			t.Errorf("want acct2 initial bal: %v, got: %v", 0, bal)
// 		}

// 		// Perform a transfer.
// 		to.Auth.Value = nil
// 		txnAmount := big.NewInt(95959)
// 		if !chain.Succeed(contract.Transfer(to.Auth, from.Addr, txnAmount)) {
// 			t.Fatal("unable to transfer")
// 		}

// 		// Recheck the balances.
// 		// Ensure contract creator's balance has decreased.
// 		if bal, err := contract.BalanceOf(&bind.CallOpts{}, to.Addr); err != nil {
// 			t.Fatal("after tx, err finding acct1 balance:", err)
// 		} else if expBal := new(big.Int).Sub(startBal, txnAmount); bal.Cmp(expBal) != 0 {
// 			t.Errorf("after tx, want acct1 bal: %v, got: %v", expBal, bal)
// 		}

// 		// Ensure other user's balance contains transferred amount.
// 		if bal, err := contract.BalanceOf(&bind.CallOpts{}, from.Addr); err != nil {
// 			t.Fatal("after tx, err finding balance:", err)
// 		} else if bal.Cmp(txnAmount) != 0 {
// 			t.Fatalf("after tx, want acct2 bal: %v, got: %v", txnAmount, bal)
// 		}
// 	})
// }
