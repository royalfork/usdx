// SPDX-License-Identifier: MIT
pragma solidity ^0.8.2;

import "./chainlink/evm-contracts/src/v0.7/interfaces/AggregatorV3Interface.sol";
import "./openzeppelin-contracts/contracts/token/ERC20/ERC20.sol";
import "./openzeppelin-contracts/contracts/access/Ownable.sol";
import "./openzeppelin-contracts/contracts/utils/math/SafeMath.sol";
import "./openzeppelin-contracts/contracts/utils/math/SafeCast.sol";

/*   $$\   $$\  $$$$$$\  $$$$$$$\  $$\   $$\
 *   $$ |  $$ |$$  __$$\ $$  __$$\ $$ |  $$ |
 *   $$ |  $$ |$$ /  \__|$$ |  $$ |\$$\ $$  |
 *   $$ |  $$ |\$$$$$$\  $$ |  $$ | \$$$$  /
 *   $$ |  $$ | \____$$\ $$ |  $$ | $$  $$<
 *   $$ |  $$ |$$\   $$ |$$ |  $$ |$$  /\$$\
 *   \$$$$$$  |\$$$$$$  |$$$$$$$  |$$ /  $$ |
 *    \______/  \______/ \_______/ \__|  \__|
 *
 *   USDX is a USD based stablecoin with the following properties:
 *   - Eth sent to the USDX contract is locked, and USDX is minted
 *     into the sender's account at the current eth/usd exchange rate.
 *   - Locked eth can be redeemed by the original sender by burning USDX
 *     at the originally minted price.
 *
 *   Note: Owner is able to set the eth/usd oracle.
 */
contract USDX is ERC20, Ownable {
	using SafeMath for uint256;
	using SafeCast for int256;
	uint8 constant FEED_DECS = 8;
	AggregatorV3Interface public usdPriceFeed;
	uint80 public priceStalenessThreshold = 0;

	struct account {
		uint256 locked; // eth
		uint256 mint;   // usdx
	}
	mapping (address => account) public accounts;

	constructor (address _priceFeed) ERC20("USDX Stablecoin", "USDX") {
		setFeed(_priceFeed);
	}

	// _newFeed's decimals parameter must immutably be set to 8.
	function setFeed(address _newFeed) public onlyOwner {
		AggregatorV3Interface newFeed = AggregatorV3Interface(_newFeed);
		require(newFeed.decimals() == FEED_DECS);
		usdPriceFeed = newFeed;
	}

	function setStalenessThreshold(uint80 _newThreshold) public onlyOwner {
		priceStalenessThreshold = _newThreshold;
	}

	// Received eth is minted into usdx at the current eth/usd
	// exchange rate, and locked in sender's account.
	// Note: msg.sender must be payable to allow unlocked of
	// deposited eth.
	receive() external payable {
		int256 xrate = rate();
		uint256 toMint = weiToUSDX(msg.value, xrate);
		account storage acct = accounts[msg.sender];
		acct.locked += msg.value;
		acct.mint += toMint;
		_mint(msg.sender, toMint);
	}

	// Unlocks _usdx amount of eth.  This method can only be called by
	// senders who have previously directly sent eth into this
	// contract.  The amount of eth unlocked is capped to the amount
	// of eth the sender has previously sent.  The amount of eth
	// unlocked is unrelated to the current ETH/USD price; it's based
	// purely on the ratio of previously sent eth and previously
	// minted usdx (ie: if 1 eth was previously received by this
	// contract to mint 1000usdx, 500 usdx is able to be unlocked for
	// .5 eth regardless of whether usd/eth price has decreased to
	// 800usd/eth).  If _usdx is 0, msg.sender's full usdx balance
	// will be used to unlock eth.
	function unlock(uint256 _usdx) public returns (uint256) {
		account storage acct = accounts[msg.sender];
		require(acct.locked > 0, "nothing to redeem");
		if (_usdx == 0) {
			_usdx = acct.mint;
		} else {
			_usdx = min(_usdx, acct.mint);
		}

		_usdx = min(_usdx, balanceOf(msg.sender));
		require(_usdx > 0, "no usdx balance");
		_burn(msg.sender, _usdx);

		uint256 unlockAmt = acct.locked.mul(_usdx).div(acct.mint);
		if (_usdx == acct.mint) {
			delete accounts[msg.sender];
		} else {
			acct.mint -= _usdx;
			acct.locked -= unlockAmt;
		}
		payable(msg.sender).transfer(unlockAmt);
		return _usdx;
	}

	// Appreciation occurs when previously locked eth appreciates in
	// usd price. The amount of appreciation can be collected as new
	// usdx, up to _limit.  A _limit of 0 collects all available
	// appreciation. To redeem the locked eth, both the principle usdx
	// mint and any collected appreciation must be returned.
	function collectAppreciation(uint256 _limit) public returns (uint256) {
		account storage acct = accounts[msg.sender];
		(bool ok, uint256 appr) = acctAppreciation(acct);
		if (!ok) {
			return 0;
		}
		if (_limit > 0) {
			appr = min(_limit, appr);
		}
		acct.mint += appr;
		_mint(msg.sender, appr);
		return appr;
	}

	// transferAcct will transfer the sender's locked eth to _to.  It
	// does not transfer any usdx balance.  _to must not already have
	// a usdx account.
	function transferAcct(address _to) public {
		require(accounts[msg.sender].locked != 0);
		require(accounts[_to].locked == 0);
		accounts[_to] = accounts[msg.sender];
		delete accounts[msg.sender];
	}

	// Returns the amount of accrued appreciation for _account.
	function appreciation(address _account) public view returns (uint256) {
		account storage acct = accounts[_account];
		(, uint256 appr) = acctAppreciation(acct);
		return appr;
	}

	function acctAppreciation(account storage _acct) private view returns (bool, uint256) {
		int256 xrate = rate();
		uint256 lockedVal = weiToUSDX(_acct.locked, xrate);
		return SafeMath.trySub(lockedVal, _acct.mint);
	}

	function rate() private view returns (int256) {
		(uint80 roundId,int256 xrate,,,uint80 answeredInRound) = usdPriceFeed.latestRoundData();
		require(roundId - answeredInRound <= priceStalenessThreshold, "stale price feed");
		return xrate;
	}

	function weiToUSDX(uint256 _wei, int256 _rate) private pure returns (uint256) {
		return _wei.mul(_rate.toUint256()).div(10**FEED_DECS);
	}

	function min(uint256 a, uint256 b) internal pure returns (uint256) {
		return a < b ? a : b;
	}
}
