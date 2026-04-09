// SPDX-License-Identifier: LGPL-3.0-only
pragma solidity ^0.8.24;

/// @title Destructible
/// @notice A minimal contract that exposes SELFDESTRUCT to any recipient.
///
///         Post EIP-6780 (Cancun / INTERSTELLAR fork):
///         - If called on a **pre-existing** contract: only the balance is
///           transferred; code and storage are preserved.
///         - If called on a contract **created in the same transaction**: the
///           contract is fully deleted (code + storage removed).
contract Destructible {
    /// @notice Transfer this contract's balance to `recipient` via SELFDESTRUCT.
    /// @param recipient The address that receives the balance.
    function destroy(address payable recipient) external {
        selfdestruct(recipient);
    }
}
