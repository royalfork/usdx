// SPDX-License-Identifier: MIT
pragma solidity ^0.8.2;

import "./chainlink/evm-contracts/src/v0.7/interfaces/AggregatorV3Interface.sol";

contract MockOracle is AggregatorV3Interface {
	struct RoundData {
		uint80 roundId;
		int256 answer;
		uint256 startedAt;
		uint256 updatedAt;
		uint80 answeredInRound;
	}

	RoundData private lastRound;

	uint8 public override immutable decimals;
	uint256 public override version;
	string public override description;
	bool public accessDenied;

	function setAccess(bool _granted) public {
		accessDenied = !_granted;
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

	function latestRoundData()
		public
		view
		virtual
		override
		checkAccess()
		returns (
			uint80 roundId, // id of current price feed round
			int256 answer, // latest trusted price
			uint256 startedAt, // start time of current round
			uint256 updatedAt, // end time of current round
			uint80 answeredInRound // round the answer was fetched in (varies due to heartbeat or staying within .5% deviation threshold)
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

	constructor () public {
		decimals = 8;
	}
}
