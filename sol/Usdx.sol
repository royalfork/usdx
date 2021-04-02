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
 *   - Eth sent to the USDX contract is minted into USDX at the current
 *     Eth/USD exchange rate.
 *
 *   Note: Owner is able to set the Eth/USD oracle.
 */
contract USDX is ERC20, Ownable {
	using SafeMath for uint256;
	using SafeCast for int256;
	uint8 constant FEED_DECS = 8;
	/* TODO: s/"ETH/USD"/"USD/ETH"? */
	// ETH/USD exchange rate feed.
	AggregatorV3Interface public ethUsdPriceFeed;
	uint8 public feedDecs;

	struct account {
		uint256 locked;
		uint256 mint;
	}
	mapping (address => account) public accounts;

	constructor (address _priceFeed) ERC20("USDX Stablecoin", "USDX") {
		setFeed(_priceFeed);
	}

	// _newFeed's decimals parameter must be immutable.
	function setFeed(address _newFeed) public onlyOwner {
		AggregatorV3Interface newFeed = AggregatorV3Interface(_newFeed);
		require(newFeed.decimals() == FEED_DECS);
		ethUsdPriceFeed = newFeed;
	}

	/* TODO: Make this external?  Is there a gas cost? */
	function transferAcct(address _to) public {
		/* TODO:  */
	}

	// Received eth is minted into USDX at the current ETH/USD
	// exchange rate.
	receive() external payable {
		(,int256 rate,,,) = ethUsdPriceFeed.latestRoundData();
		uint256 toMint = weiToUSDX(msg.value, rate);
		account storage acct = accounts[msg.sender];
		acct.locked += msg.value;
		acct.mint += toMint;
		_mint(msg.sender, toMint);
	}


	// Redeems USDX back into eth.  Amount of eth received is based on
	function redeem(uint256 _amount) public returns (uint256) {
		account storage acct = accounts[msg.sender];
		require(acct.locked != 0, "nothing to redeem");
		if (_amount == 0) {
			_amount = acct.mint;
		} else {
			_amount = _amount < acct.mint ? _amount : acct.mint;
		}

		_burn(msg.sender, _amount);

		uint256 unlock = acct.locked.mul(_amount).div(acct.mint);
		if (_amount == acct.mint) {
			delete accounts[msg.sender];
		} else {
			acct.mint -= _amount;
			acct.locked -= unlock;
		}
		payable(msg.sender).transfer(unlock);
		return _amount;
	}

	function collectAppreciation(uint256 _limit) public returns (uint256) {
		account storage acct = accounts[msg.sender];
		(bool ok, uint256 appr) = acctAppreciation(acct);
		if (!ok) {
			return 0;
		}
		if (_limit > 0) {
			appr = _limit < appr ? _limit : appr;
		}
		acct.mint += appr;
		_mint(msg.sender, appr);
		return appr;
	}

	function appreciation() public view returns (uint256) {
		account storage acct = accounts[msg.sender];
		(, uint256 appr) = acctAppreciation(acct);
		return appr;
	}

	function acctAppreciation(account storage acct) private view returns (bool, uint256) {
		(,int256 rate,,,) = ethUsdPriceFeed.latestRoundData();
		uint256 lockedVal = weiToUSDX(acct.locked, rate);
		return SafeMath.trySub(lockedVal, acct.mint);
	}

	function weiToUSDX(uint256 _wei, int256 _rate) private view returns (uint256) {
		return _wei.mul(_rate.toUint256()).div(10**FEED_DECS);
	}
}
