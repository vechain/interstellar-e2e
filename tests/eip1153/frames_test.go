package eip1153

// EIP-1153 semantics that only appear once more than one call frame is
// involved. transient_storage_test.go covers the single-frame opcode behaviour;
// this file covers the four rules that need a caller and a callee:
//
//	(1) TSTORE raises an exception under STATICCALL; TLOAD is permitted.
//	(2) A reverting frame rolls back its transient writes, including those made
//	    by its inner calls.
//	(3) CALL/STATICCALL make the callee the owner of the transient storage;
//	    DELEGATECALL makes the caller the owner.
//	(4) Transient storage survives across frames for the whole transaction.
//
// All of these are intra-transaction properties, so they are exercised through
// InspectClauses: one simulated clause per test, with the probe contract making
// the nested calls internally.
//
// Not covered: CALLCODE, which the specification names alongside DELEGATECALL.
// Solidity has emitted no CALLCODE since 0.5.0, so reaching it would mean
// hand-assembling bytecode for a rule DELEGATECALL already pins down.

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vechain/interstellar-e2e/tests/eip1153/contracts/generated/transientprobe"
)

// ---------------------------------------------------------------------------
// (1) STATICCALL restriction
// ---------------------------------------------------------------------------

// TestEIP1153_StaticCall_TSTOREIsException checks the one hard prohibition in
// the specification:
//
//	"If the TSTORE opcode is called within the context of a STATICCALL, it will
//	 result in an exception instead of performing the modification."
//
// The probe sends one identical payload to write(k, v) twice — once by CALL,
// once by STATICCALL — and reports both outcomes plus the returndata size.
//
// The CALL leg is the control, and it is what makes this test mean anything: a
// STATICCALL to an unknown selector would also come back as "failed, no
// returndata", so a bare STATICCALL failure would be indistinguishable from a
// typo. Asserting the returndata size is separately what makes this a test of
// "exception" rather than merely "did not succeed" — an exception returns
// nothing, a plain revert would carry a 4-byte error selector.
func TestEIP1153_StaticCall_TSTOREIsException(t *testing.T) {
	_, probeB := deployedProbes(t)

	data := transientprobe.Methods().CallThenStaticWriteMethod().MustPack(
		probeAddr(probeB), big.NewInt(7), big.NewInt(0xBEEF))

	result := callProbe(t, probeA, data, 300_000)
	require.False(t, result.Reverted,
		"the outer frame must survive to report the inner failure (vmError: %s)", result.VMError)

	words := decodeWords(t, result.Data, 3)
	callOk, staticOk, staticRetLen := words[0], words[1], words[2]

	require.Equal(t, uint64(1), callOk.Uint64(),
		"control: the same payload sent by CALL must succeed, otherwise the STATICCALL "+
			"result below says nothing about TSTORE")

	assert.Equal(t, uint64(0), staticOk.Uint64(),
		"TSTORE inside a STATICCALL must fail, but the inner call reported success")
	assert.Equal(t, uint64(0), staticRetLen.Uint64(),
		"TSTORE inside a STATICCALL must raise an exception, which returns no data; "+
			"%d bytes of returndata means it reverted normally instead", staticRetLen.Uint64())
}

// TestEIP1153_StaticCall_TLOADAllowed covers the other half of the same
// sentence: "TLOAD is allowed within the context of a STATICCALL."
//
// The probe first CALLs probeB.write(k, v) — a normal call, so probeB owns and
// keeps the value — then reads the same slot back through a STATICCALL. Reading
// a non-zero value proves both that TLOAD executed under STATICCALL and that it
// saw the right namespace.
func TestEIP1153_StaticCall_TLOADAllowed(t *testing.T) {
	_, probeB := deployedProbes(t)

	const value = 0xC0FFEE
	data := transientprobe.Methods().WriteThenStaticReadMethod().MustPack(
		probeAddr(probeB), big.NewInt(9), big.NewInt(value))

	result := callProbe(t, probeA, data, 300_000)
	require.False(t, result.Reverted, "writeThenStaticRead must not revert (vmError: %s)", result.VMError)

	words := decodeWords(t, result.Data, 2)
	ok, got := words[0], words[1]

	assert.Equal(t, uint64(1), ok.Uint64(), "TLOAD under STATICCALL must succeed")
	assert.Equal(t, uint64(value), got.Uint64(),
		"TLOAD under STATICCALL must return the value the preceding CALL stored")
}

// ---------------------------------------------------------------------------
// (2) Revert rollback
// ---------------------------------------------------------------------------

// TestEIP1153_Revert_RollsBackFrameWrites covers the first half of:
//
//	"If a frame reverts, all writes to transient storage that took place between
//	 entry to the frame and the return are reverted..."
//
// The probe writes `pre`, self-CALLs a function that writes `inner` and then
// reverts, and swallows the failure. Caller and callee are the same address, so
// both writes hit the same transient slot: without rollback the final read
// would return `inner`.
func TestEIP1153_Revert_RollsBackFrameWrites(t *testing.T) {
	deployedProbes(t)

	const (
		pre   = 0x1111
		inner = 0x2222
	)
	data := transientprobe.Methods().RollbackAfterInnerRevertMethod().MustPack(
		big.NewInt(1), big.NewInt(pre), big.NewInt(inner))

	result := callProbe(t, probeA, data, 300_000)
	require.False(t, result.Reverted,
		"rollbackAfterInnerRevert must not revert — it swallows the inner failure (vmError: %s)",
		result.VMError)

	got := decodeWords(t, result.Data, 1)[0]
	assert.Equal(t, uint64(pre), got.Uint64(),
		"a reverting frame's transient write must be rolled back: expected the pre-call value 0x%x, got 0x%x",
		uint64(pre), got.Uint64())
}

// TestEIP1153_Revert_RollsBackInnerCallWrites covers the clause the previous
// test does not reach: "...including those that took place in inner calls."
//
// The reverting frame writes `a`, then makes a *successful* inner call that
// writes `b`, and only then reverts. The successful inner call's write must be
// undone too, so the surviving value is still `pre`.
func TestEIP1153_Revert_RollsBackInnerCallWrites(t *testing.T) {
	deployedProbes(t)

	const (
		pre = 0x1111
		a   = 0x2222
		b   = 0x3333
	)
	data := transientprobe.Methods().RollbackIncludesInnerCallWritesMethod().MustPack(
		big.NewInt(2), big.NewInt(pre), big.NewInt(a), big.NewInt(b))

	result := callProbe(t, probeA, data, 400_000)
	require.False(t, result.Reverted,
		"rollbackIncludesInnerCallWrites must not revert (vmError: %s)", result.VMError)

	got := decodeWords(t, result.Data, 1)[0]
	assert.Equal(t, uint64(pre), got.Uint64(),
		"a revert must roll back writes made by the reverting frame (0x%x) and by its "+
			"successful inner calls (0x%x); expected 0x%x, got 0x%x",
		uint64(a), uint64(b), uint64(pre), got.Uint64())
}

// ---------------------------------------------------------------------------
// (3) Ownership
// ---------------------------------------------------------------------------

// TestEIP1153_Ownership_CallCalleeOwns checks that under CALL "the owning
// contract of the transient storage is the contract that is the target of the
// CALL or STATICCALL instruction (the callee)".
//
// probeA writes `mine` into its own slot k, then CALLs probeB.write(k, theirs).
// Both values must survive at their own address. Reading back both sides is
// what gives the test teeth: if the namespaces were shared, probeA's slot would
// come back holding `theirs`.
func TestEIP1153_Ownership_CallCalleeOwns(t *testing.T) {
	_, probeB := deployedProbes(t)

	const (
		mine   = 0xAAAA
		theirs = 0xBBBB
	)
	data := transientprobe.Methods().WriteThenCallOtherWriteMethod().MustPack(
		probeAddr(probeB), big.NewInt(3), big.NewInt(mine), big.NewInt(theirs))

	result := callProbe(t, probeA, data, 400_000)
	require.False(t, result.Reverted,
		"writeThenCallOtherWrite must not revert (vmError: %s)", result.VMError)

	words := decodeWords(t, result.Data, 2)
	ourValue, theirValue := words[0], words[1]

	assert.Equal(t, uint64(mine), ourValue.Uint64(),
		"a CALL writes into the callee's namespace, so the caller's own slot must be untouched")
	assert.Equal(t, uint64(theirs), theirValue.Uint64(),
		"the callee must own and keep the value its own TSTORE wrote")
}

// TestEIP1153_Ownership_DelegateCallCallerOwns checks the inverse rule: under
// DELEGATECALL "the owning contract of the transient storage is the contract
// that issued the DELEGATECALL or CALLCODE instruction (the caller)".
//
// probeA DELEGATECALLs probeB's write(k, v). The code runs in probeA's context,
// so the value must land in probeA's namespace and probeB's must stay zero.
func TestEIP1153_Ownership_DelegateCallCallerOwns(t *testing.T) {
	_, probeB := deployedProbes(t)

	const value = 0xD00D
	data := transientprobe.Methods().DelegateWriteMethod().MustPack(
		probeAddr(probeB), big.NewInt(4), big.NewInt(value))

	result := callProbe(t, probeA, data, 400_000)
	require.False(t, result.Reverted, "delegateWrite must not revert (vmError: %s)", result.VMError)

	words := decodeWords(t, result.Data, 2)
	ourValue, implValue := words[0], words[1]

	assert.Equal(t, uint64(value), ourValue.Uint64(),
		"under DELEGATECALL the caller owns the transient storage, so the write must land here")
	assert.Equal(t, uint64(0), implValue.Uint64(),
		"the DELEGATECALL implementation's own namespace must stay untouched, got 0x%x",
		implValue.Uint64())
}

// ---------------------------------------------------------------------------
// (4) Survival across frames within one transaction
// ---------------------------------------------------------------------------

// TestEIP1153_SurvivesAcrossFrames_Reentrancy exercises the pattern EIP-1153
// exists for. probeA writes slot k, calls probeB, and probeB calls straight back
// into probeA to read k. The re-entrant frame must observe the value — that
// visibility is exactly what a transient re-entrancy guard depends on.
//
// This complements TestTransientStorage_ClearedBetweenTransactions, which pins
// the other end of the lifetime: values are gone in the *next* transaction.
func TestEIP1153_SurvivesAcrossFrames_Reentrancy(t *testing.T) {
	_, probeB := deployedProbes(t)

	const value = 0xFEED
	data := transientprobe.Methods().ReentrantReadMethod().MustPack(
		probeAddr(probeB), big.NewInt(5), big.NewInt(value))

	result := callProbe(t, probeA, data, 500_000)
	require.False(t, result.Reverted, "reentrantRead must not revert (vmError: %s)", result.VMError)

	got := decodeWords(t, result.Data, 1)[0]
	assert.Equal(t, uint64(value), got.Uint64(),
		"a re-entrant frame must see the transient value written by the outer frame")
}

// TestEIP1153_SurvivesAcrossFrames_InnerWritePersists is the mirror image of the
// rollback tests: when an inner call returns *successfully*, its transient
// writes stay visible to the caller. Without this the rollback tests would pass
// trivially on an implementation that simply discarded every inner write.
func TestEIP1153_SurvivesAcrossFrames_InnerWritePersists(t *testing.T) {
	deployedProbes(t)

	const value = 0x5A5A
	data := transientprobe.Methods().InnerWritePersistsMethod().MustPack(
		big.NewInt(6), big.NewInt(value))

	result := callProbe(t, probeA, data, 300_000)
	require.False(t, result.Reverted, "innerWritePersists must not revert (vmError: %s)", result.VMError)

	got := decodeWords(t, result.Data, 1)[0]
	assert.Equal(t, uint64(value), got.Uint64(),
		"a successful inner call's transient write must remain visible to the caller")
}
