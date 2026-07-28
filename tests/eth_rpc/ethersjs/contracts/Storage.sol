// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract Storage {
    event Set(address indexed who, uint256 value);

    uint256 public value;

    function set(uint256 v) external {
        value = v;
        emit Set(msg.sender, v);
    }

    function get() external view returns (uint256) {
        return value;
    }

    function setStrict(uint256 v) external {
        require(v != 0, "value must be non-zero");
        value = v;
        emit Set(msg.sender, v);
    }

    error MustBeNonZero(uint256 given);

    function setStrictCustomError(uint256 v) external {
        if (v == 0) revert MustBeNonZero(v);
        value = v;
        emit Set(msg.sender, v);
    }

    event Tipped(address indexed who, uint256 amount);

    function tip() external payable {
        emit Tipped(msg.sender, msg.value);
    }

    function totalTipped() external view returns (uint256) {
        return address(this).balance;
    }
}
