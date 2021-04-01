// SPDX-License-Identifier: MIT
pragma solidity ^0.8.2;

import "./chainlink/evm-contracts/src/v0.6/interfaces/AggregatorV3Interface.sol";
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
 *   - Received eth is then burned forever.
 *
 *   Note: Owner is able to set the Eth/USD oracle.
 */
contract USDX is ERC20, Ownable {
	using SafeMath for uint256;
	using SafeCast for int256;
	uint8 constant FEED_DECS = 8;
	// ETH/USD exchange rate feed.
	AggregatorV3Interface public ethUsdPriceFeed;

	constructor (address _priceFeed) public ERC20("USDX Stablecoin", "USDX") {
		setFeed(_priceFeed);
	}

	function setFeed(address _newFeed) public onlyOwner {
		AggregatorV3Interface newFeed = AggregatorV3Interface(_newFeed);
		require(newFeed.decimals() == FEED_DECS);
		ethUsdPriceFeed = newFeed;
	}

	receive() external payable {
		(,int256 rate,,,) = ethUsdPriceFeed.latestRoundData();
		uint256 usd = msg.value.mul(rate.toUint256()).div(10**FEED_DECS); //latestRoundData uses 8 decimals
		_mint(_msgSender(), usd);
	}
}
