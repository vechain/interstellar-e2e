// SPDX-License-Identifier: LGPL-3.0-only
pragma solidity ^0.8.24;

import "./Destructible.sol";

/// @title Factory
/// @notice Deploys a Destructible child and immediately calls SELFDESTRUCT on it
///         within the same transaction.
///
///         This exercises the EIP-6780 **same-transaction** deletion path:
///         because the child was created in the same tx as the SELFDESTRUCT call,
///         it is fully deleted (code + storage removed) post-INTERSTELLAR fork.
contract Factory {
    /// @notice Deploy a child Destructible and self-destruct it in the same tx.
    /// @return child The address of the newly created (and immediately destroyed) child.
    function deployAndDestroy() external returns (address child) {
        Destructible d = new Destructible();
        child = address(d);
        d.destroy(payable(msg.sender));
    }
}
