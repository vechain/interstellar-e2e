// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract Create2Factory {
    event Deployed(address addr);

    function deploy(bytes32 salt, bytes calldata initCode) external returns (address addr) {
        assembly {
            let memPtr := mload(0x40)
            calldatacopy(memPtr, initCode.offset, initCode.length)
            addr := create2(0, memPtr, initCode.length, salt)
        }
        require(addr != address(0), "create2 failed");
        emit Deployed(addr);
    }
}
