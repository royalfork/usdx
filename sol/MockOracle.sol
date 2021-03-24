// SPDX-License-Identifier: MIT
pragma solidity ^0.8.2;

import "./chainlink/evm-contracts/src/v0.6/interfaces/AggregatorV3Interface.sol";

contract MockOracle is AggregatorV3Interface {
	struct RoundData {
		uint80 roundId;
		int256 answer;
		uint256 startedAt;
		uint256 updatedAt;
		uint80 answeredInRound;
	}

	RoundData private lastRound;

	uint8 public override decimals;
	uint256 public override version;
	string public override description;
	bool public accessDenied;

	function setAccess(bool _granted) public {
		accessDenied = !_granted;
	}

	// TODO figure out what decimals is for live ETH/USD oracle, and
	// test against that.  ALSO, test against changes to make sure
	// math is ok.
	// TODO in tests, use theoretical and live data.
	function setDecimals (uint8 _decimals) public {
		decimals = _decimals;
	}

	function setLastRound(
		uint80 _roundId,
		int256 _answer,
		uint256 _startedAt,
		uint256 _updatedAt,
		uint80 _answeredInRound
	) public {
		lastRound.roundId = _roundId;
		lastRound.answer = _answer;
		lastRound.startedAt = _startedAt;
		lastRound.updatedAt = _updatedAt;
		lastRound.answeredInRound = _answeredInRound;
	}

	// What do all the params actually mean?
	function latestRoundData()
		public
		view
		virtual
		override
		checkAccess()
		returns (
			uint80 roundId,
			int256 answer,
			uint256 startedAt,
			uint256 updatedAt,
			uint80 answeredInRound
		)
	{
		return (lastRound.roundId,
				lastRound.answer,
				lastRound.startedAt,
				lastRound.updatedAt,
				lastRound.answeredInRound);
	}

	function getRoundData(uint80 _roundId)
		public
		view
		override
		returns (
			uint80 roundId,
			int256 answer,
			uint256 startedAt,
			uint256 updatedAt,
			uint80 answeredInRound
		)
	{
		return (_roundId, 0, 0, 0, 0);
	}

	modifier checkAccess() {
		require(!accessDenied, "No access");
		_;
	}

	constructor () public { }
}
