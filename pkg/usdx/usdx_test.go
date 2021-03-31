//go:generate abigen --sol ../../sol/Usdx.sol --pkg usdx --out usdx_abigen.go --exc ../../sol/chainlink/evm-contracts/src/v0.6/interfaces/AggregatorV3Interface.sol:AggregatorV3Interface
package usdx

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/royalfork/soltest"
)

func TestUsdx(t *testing.T) {
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

	t.Run("constructorSetsPriceFeed", func(t *testing.T) {
		priceFeed, err := contract.EthUsdPriceFeed(&bind.CallOpts{})
		if err != nil {
			t.Errorf("uexpected err reading ethUsdPriceFeed: %v", err)
		}
		if priceFeed != oracleAddr {
			t.Errorf("want ethUsdPriceFeed: %v, got: %v", oracleAddr, priceFeed)
		}
	})

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

	newOracleAddr, _, _, err := DeployMockOracle(accts[0].Auth, chain)

	if err != nil {
		t.Fatal(err)
	}

	t.Run("feedDecimalsMatch", func(t *testing.T) {

		newOracle := common.Address{0xff}

		if chain.Succeed(contract.SetFeed(accts[0].Auth, newOracle)) {
			t.Fatal("feed with incorrect decimals (!= 8) should not be set")
		}

	})

	t.Run("ownerReplacesFeed", func(t *testing.T) {

		if chain.Succeed(contract.SetFeed(accts[1].Auth, newOracleAddr)) {
			t.Fatal("non-owner shouldn't replace feed")
		}

		// owner can change
		if !chain.Succeed(contract.SetFeed(accts[0].Auth, newOracleAddr)) {
			t.Error("owner should setFeed")
		}

		if feed, err := contract.EthUsdPriceFeed(&bind.CallOpts{}); err != nil {
			t.Fatal("unable to get feed")
		} else if feed != newOracleAddr {
			t.Errorf("want feed: %s, got: %s", newOracleAddr, feed)
		}

		// owner changes feed back
		if !chain.Succeed(contract.SetFeed(accts[0].Auth, oracleAddr)) {
			t.Fatal("owner should setFeed")
		}
	})

	t.Run("feedRevertsOnMint", func(t *testing.T) {
		if !chain.Succeed(oracleContract.SetAccess(accts[0].Auth, false)) {
			t.Fatal("unable to set oracle access")
		}

		accts[1].Auth.Value = big.NewInt(100)
		if chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
			t.Error("shouldn't mint when feed reverts")
		}

		if !chain.Succeed(oracleContract.SetAccess(accts[0].Auth, true)) {
			t.Fatal("unable to set oracle access")
		}

		if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
			t.Error("should mint when feed doesn't revert")
		}
	})

	t.Run("mintOnReceive", func(t *testing.T) {
		var payments = []*big.Int{
			big.NewInt(params.Wei),
			new(big.Int).Mul(big.NewInt(10), big.NewInt(params.Wei)),
			new(big.Int).Mul(big.NewInt(200), big.NewInt(params.Wei)),
			new(big.Int).Mul(big.NewInt(3000), big.NewInt(params.Wei)),
			big.NewInt(params.GWei),
			new(big.Int).Mul(big.NewInt(10), big.NewInt(params.GWei)),
			new(big.Int).Mul(big.NewInt(200), big.NewInt(params.GWei)),
			new(big.Int).Mul(big.NewInt(3000), big.NewInt(params.GWei)),
			big.NewInt(params.Ether),
			new(big.Int).Mul(big.NewInt(10), big.NewInt(params.Ether)),
			new(big.Int).Mul(big.NewInt(200), big.NewInt(params.Ether)),
			new(big.Int).Mul(big.NewInt(3000), big.NewInt(params.Ether)),
		}

		var rates = []int64{
			1,
			2e6,
			1e8, // 1 eth = 1 usd, at 8 decimals
			3e10,
			1e18,
			58973604819,
			589736048190,
			58973604819000,
		}

		var rateDec = uint8(8)

		txEvts := make(chan *USDXTransfer, 1)
		txSub, err := contract.USDXFilterer.WatchTransfer(nil, txEvts, nil, nil)
		if err != nil {
			t.Fatal("unable to watch for transfer events")
		}
		defer txSub.Unsubscribe()

		// returns receiving address and amount of last transfer operation.
		transferEvt := func() (to common.Address, value *big.Int, err error) {
			select {
			case e := <-txEvts:
				return e.To, e.Value, nil

			case err := <-txSub.Err():
				return common.Address{}, nil, err

			case <-time.After(time.Second):
				return common.Address{}, nil, errors.New("timeout")
			}
		}

		expAmount := func(rate int64, rateDec uint8, payment *big.Int) *big.Int {
			// TODO refactor this so it's actually readable
			res := new(big.Int).Mul(big.NewInt(rate), payment)
			return res.Div(res, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(rateDec)), nil))
		}

		zero := new(big.Int)
		for _, rate := range rates {
			for _, payment := range payments {
				// set rate in oracle
				if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, big.NewInt(rate), zero, zero, zero)) {
					t.Fatalf("unable to set oracle round: rate=%v", rate)
				}

				accts[1].Auth.Value = payment
				if !chain.Succeed((&USDXRaw{contract}).Transfer(accts[1].Auth)) {
					t.Fatal("unable to transfer")
				}

				to, amt, err := transferEvt()
				if err != nil {
					t.Error(err)
				}
				if to != accts[1].Addr {
					t.Errorf("want transfer addr: %v, got: %v", accts[1], to)
				}
				if expAmt := expAmount(rate, rateDec, payment); amt.Cmp(expAmt) != 0 {
					t.Errorf("rate=%d, rateDec=%d, payment=%v. want amount: %v, got: %v", rate, rateDec, payment, expAmt, amt)
				}
			}
		}
	})

	t.Run("erc20Transfer", func(t *testing.T) {
		var (
			to, from = accts[2], accts[3]
			rate     = big.NewInt(100_000_000_000)                                // 1,000 usd = 1 eth
			send     = new(big.Int).Mul(big.NewInt(10), big.NewInt(params.Ether)) // send 10 eth
			expBal   = big.NewInt(10_000)                                         // expect 10,000 usdx

			zero = new(big.Int)
		)

		// set rate in oracle
		if !chain.Succeed(oracleContract.SetLastRound(accts[0].Auth, zero, rate, zero, zero, zero)) {
			t.Fatalf("unable to set oracle round: rate=%v", rate)
		}

		to.Auth.Value = send
		if !chain.Succeed((&USDXRaw{contract}).Transfer(to.Auth)) {
			t.Fatal("unable to transfer")
		}

		// Ensure contract creator has expected balance.
		usdxDec, err := contract.Decimals(&bind.CallOpts{})
		if err != nil {
			t.Fatal("unable to get usdx decimals")
		}

		startBal := new(big.Int).Mul(expBal, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(usdxDec)), nil))
		if bal, err := contract.BalanceOf(&bind.CallOpts{}, to.Addr); err != nil {
			t.Fatal("err finding acct1 balance:", err)
		} else if bal.Cmp(startBal) != 0 {
			t.Errorf("want acct1 initial bal: %v, got: %v", startBal, bal)
		}

		// Ensure other user has 0 balance.
		if bal, err := contract.BalanceOf(&bind.CallOpts{}, from.Addr); err != nil {
			t.Fatal("err finding acct2 balance:", err)
		} else if bal.Uint64() != 0 {
			t.Errorf("want acct2 initial bal: %v, got: %v", 0, bal)
		}

		// Perform a transfer.
		to.Auth.Value = nil
		txnAmount := big.NewInt(95959)
		if !chain.Succeed(contract.Transfer(to.Auth, from.Addr, txnAmount)) {
			t.Fatal("unable to transfer")
		}

		// Recheck the balances.
		// Ensure contract creator's balance has decreased.
		if bal, err := contract.BalanceOf(&bind.CallOpts{}, to.Addr); err != nil {
			t.Fatal("after tx, err finding acct1 balance:", err)
		} else if expBal := new(big.Int).Sub(startBal, txnAmount); bal.Cmp(expBal) != 0 {
			t.Errorf("after tx, want acct1 bal: %v, got: %v", expBal, bal)
		}

		// Ensure other user's balance contains transferred amount.
		if bal, err := contract.BalanceOf(&bind.CallOpts{}, from.Addr); err != nil {
			t.Fatal("after tx, err finding balance:", err)
		} else if bal.Cmp(txnAmount) != 0 {
			t.Fatalf("after tx, want acct2 bal: %v, got: %v", txnAmount, bal)
		}
	})
}
