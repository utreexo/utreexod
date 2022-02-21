// Copyright (c) 2013-2017 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txscript

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/wire"
)

// shallowCopyTx creates a shallow copy of the transaction for use when
// calculating the signature hash.  It is used over the Copy method on the
// transaction itself since that is a deep copy and therefore does more work and
// allocates much more space than needed.
func shallowCopyTx(tx *wire.MsgTx) wire.MsgTx {
	// As an additional memory optimization, use contiguous backing arrays
	// for the copied inputs and outputs and point the final slice of
	// pointers into the contiguous arrays.  This avoids a lot of small
	// allocations.
	txCopy := wire.MsgTx{
		Version:  tx.Version,
		TxIn:     make([]*wire.TxIn, len(tx.TxIn)),
		TxOut:    make([]*wire.TxOut, len(tx.TxOut)),
		LockTime: tx.LockTime,
	}
	txIns := make([]wire.TxIn, len(tx.TxIn))
	for i, oldTxIn := range tx.TxIn {
		txIns[i] = *oldTxIn
		txCopy.TxIn[i] = &txIns[i]
	}
	txOuts := make([]wire.TxOut, len(tx.TxOut))
	for i, oldTxOut := range tx.TxOut {
		txOuts[i] = *oldTxOut
		txCopy.TxOut[i] = &txOuts[i]
	}
	return txCopy
}

// CalcSignatureHash will, given a script and hash type for the current script
// engine instance, calculate the signature hash to be used for signing and
// verification.
//
// NOTE: This function is only valid for version 0 scripts. Since the function
// does not accept a script version, the results are undefined for other script
// versions.
func CalcSignatureHash(script []byte, hashType SigHashType, tx *wire.MsgTx, idx int) ([]byte, error) {
	const scriptVersion = 0
	if err := checkScriptParses(scriptVersion, script); err != nil {
		return nil, err
	}

	return calcSignatureHash(script, hashType, tx, idx), nil
}

// calcSignatureHash computes the signature hash for the specified input of the
// target transaction observing the desired signature hash type.
func calcSignatureHash(sigScript []byte, hashType SigHashType, tx *wire.MsgTx, idx int) []byte {
	// The SigHashSingle signature type signs only the corresponding input
	// and output (the output with the same index number as the input).
	//
	// Since transactions can have more inputs than outputs, this means it
	// is improper to use SigHashSingle on input indices that don't have a
	// corresponding output.
	//
	// A bug in the original Satoshi client implementation means specifying
	// an index that is out of range results in a signature hash of 1 (as a
	// uint256 little endian).  The original intent appeared to be to
	// indicate failure, but unfortunately, it was never checked and thus is
	// treated as the actual signature hash.  This buggy behavior is now
	// part of the consensus and a hard fork would be required to fix it.
	//
	// Due to this, care must be taken by software that creates transactions
	// which make use of SigHashSingle because it can lead to an extremely
	// dangerous situation where the invalid inputs will end up signing a
	// hash of 1.  This in turn presents an opportunity for attackers to
	// cleverly construct transactions which can steal those coins provided
	// they can reuse signatures.
	if hashType&sigHashMask == SigHashSingle && idx >= len(tx.TxOut) {
		var hash chainhash.Hash
		hash[0] = 0x01
		return hash[:]
	}

	// Remove all instances of OP_CODESEPARATOR from the script.
	sigScript = removeOpcodeRaw(sigScript, OP_CODESEPARATOR)

	// The calculated checksum here will be the sigHash. We'll be adding necessary
	// data in for different types of signature hash types.
	sigHash := sha256.New()
	binary.Write(sigHash, binary.LittleEndian, uint32(tx.Version))

	// Add inputs to the hash.
	if (hashType & SigHashAnyOneCanPay) == 0 {
		txInCount := uint64(len(tx.TxIn))
		wire.WriteVarInt(sigHash, 0, txInCount)
		for i := range tx.TxIn {
			sigHash.Write(tx.TxIn[i].PreviousOutPoint.Hash[:])
			binary.Write(sigHash, binary.LittleEndian, tx.TxIn[i].PreviousOutPoint.Index)

			// If the txIn is the specified idx, write the actual sigScript and the sequence.
			if i == idx {
				wire.WriteVarBytes(sigHash, 0, sigScript)
				binary.Write(sigHash, binary.LittleEndian, tx.TxIn[i].Sequence)
			} else {
				wire.WriteVarBytes(sigHash, 0, nil)
				// For SigHashNone and SigHashSingle, don't write the actual sequence and
				// just write 0.
				if hashType&sigHashMask == SigHashNone || hashType&sigHashMask == SigHashSingle {
					binary.Write(sigHash, binary.LittleEndian, uint32(0))
				} else {
					binary.Write(sigHash, binary.LittleEndian, tx.TxIn[i].Sequence)
				}
			}
		}
	} else {
		// Write count of 1
		wire.WriteVarInt(sigHash, 0, uint64(1))
		wire.WriteOutPoint(sigHash, 0, 0, &tx.TxIn[idx].PreviousOutPoint)
		wire.WriteVarBytes(sigHash, 0, sigScript)
		binary.Write(sigHash, binary.LittleEndian, tx.TxIn[idx].Sequence)
	}

	// Add outputs to the hash.
	if hashType&sigHashMask == SigHashNone {
		// Write count of 0 for SigHashNone
		wire.WriteVarInt(sigHash, 0, uint64(0))
	} else if hashType&sigHashMask == SigHashSingle {
		// Write count of all txOuts. We count all txOuts up til the idx specified.
		wire.WriteVarInt(sigHash, 0, uint64(idx+1))

		// Make empty txOut that'll be used for all txOuts except for the
		// specified idx.
		to := wire.TxOut{
			Value:    -1,
			PkScript: nil,
		}

		// For all txOuts before the specified idx, write empty txOuts.
		for i := 0; i < idx; i++ {
			wire.WriteTxOut(sigHash, 0, 0, &to)
		}

		// Finally write the value and the pkscript.
		binary.Write(sigHash, binary.LittleEndian, uint64(tx.TxOut[idx].Value))
		wire.WriteVarBytes(sigHash, 0, tx.TxOut[idx].PkScript)
	} else {
		txOutCount := uint64(len(tx.TxOut))
		wire.WriteVarInt(sigHash, 0, txOutCount)
		for i := range tx.TxOut {
			binary.Write(sigHash, binary.LittleEndian, uint64(tx.TxOut[i].Value))
			wire.WriteVarBytes(sigHash, 0, tx.TxOut[i].PkScript)
		}
	}

	// Finally, write out the transaction's locktime, and the sig hash type.
	var bLockTime [4]byte
	binary.LittleEndian.PutUint32(bLockTime[:], tx.LockTime)
	sigHash.Write(bLockTime[:])
	var bHashType [4]byte
	binary.LittleEndian.PutUint32(bHashType[:], uint32(hashType))
	sigHash.Write(bHashType[:])
	return chainhash.DoubleHashRaw(sigHash)
}

// calcWitnessSignatureHashRaw computes the sighash digest of a transaction's
// segwit input using the new, optimized digest calculation algorithm defined
// in BIP0143: https://github.com/bitcoin/bips/blob/master/bip-0143.mediawiki.
// This function makes use of pre-calculated sighash fragments stored within
// the passed HashCache to eliminate duplicate hashing computations when
// calculating the final digest, reducing the complexity from O(N^2) to O(N).
// Additionally, signatures now cover the input value of the referenced unspent
// output. This allows offline, or hardware wallets to compute the exact amount
// being spent, in addition to the final transaction fee. In the case the
// wallet if fed an invalid input amount, the real sighash will differ causing
// the produced signature to be invalid.
func calcWitnessSignatureHashRaw(scriptSig []byte, sigHashes *TxSigHashes,
	hashType SigHashType, tx *wire.MsgTx, idx int, amt int64) ([]byte, error) {

	// As a sanity check, ensure the passed input index for the transaction
	// is valid.
	if idx > len(tx.TxIn)-1 {
		return nil, fmt.Errorf("idx %d but %d txins", idx, len(tx.TxIn))
	}

	// We'll utilize this buffer throughout to incrementally calculate
	// the signature hash for this transaction.
	sigHash := sha256.New()

	// First write out, then encode the transaction's version number.
	var bVersion [4]byte
	binary.LittleEndian.PutUint32(bVersion[:], uint32(tx.Version))
	sigHash.Write(bVersion[:])

	// Next write out the possibly pre-calculated hashes for the sequence
	// numbers of all inputs, and the hashes of the previous outs for all
	// outputs.
	var zeroHash chainhash.Hash

	// If anyone can pay isn't active, then we can use the cached
	// hashPrevOuts, otherwise we just write zeroes for the prev outs.
	if hashType&SigHashAnyOneCanPay == 0 {
		sigHash.Write(sigHashes.HashPrevOuts[:])
	} else {
		sigHash.Write(zeroHash[:])
	}

	// If the sighash isn't anyone can pay, single, or none, the use the
	// cached hash sequences, otherwise write all zeroes for the
	// hashSequence.
	if hashType&SigHashAnyOneCanPay == 0 &&
		hashType&sigHashMask != SigHashSingle &&
		hashType&sigHashMask != SigHashNone {
		sigHash.Write(sigHashes.HashSequence[:])
	} else {
		sigHash.Write(zeroHash[:])
	}

	txIn := tx.TxIn[idx]

	// Next, write the outpoint being spent.
	sigHash.Write(txIn.PreviousOutPoint.Hash[:])
	var bIndex [4]byte
	binary.LittleEndian.PutUint32(bIndex[:], txIn.PreviousOutPoint.Index)
	sigHash.Write(bIndex[:])

	if isWitnessPubKeyHashScript(scriptSig) {
		// The script code for a p2wkh is a length prefix varint for
		// the next 25 bytes, followed by a re-creation of the original
		// p2pkh pk script.
		sigHash.Write([]byte{0x19})
		sigHash.Write([]byte{OP_DUP})
		sigHash.Write([]byte{OP_HASH160})
		sigHash.Write([]byte{OP_DATA_20})
		sigHash.Write(extractWitnessPubKeyHash(scriptSig))
		sigHash.Write([]byte{OP_EQUALVERIFY})
		sigHash.Write([]byte{OP_CHECKSIG})
	} else {
		// For p2wsh outputs, and future outputs, the script code is
		// the original script, with all code separators removed,
		// serialized with a var int length prefix.
		wire.WriteVarBytes(sigHash, 0, scriptSig)
	}

	// Next, add the input amount, and sequence number of the input being
	// signed.
	var bAmount [8]byte
	binary.LittleEndian.PutUint64(bAmount[:], uint64(amt))
	sigHash.Write(bAmount[:])
	var bSequence [4]byte
	binary.LittleEndian.PutUint32(bSequence[:], txIn.Sequence)
	sigHash.Write(bSequence[:])

	// If the current signature mode isn't single, or none, then we can
	// re-use the pre-generated hashoutputs sighash fragment. Otherwise,
	// we'll serialize and add only the target output index to the signature
	// pre-image.
	if hashType&SigHashSingle != SigHashSingle &&
		hashType&SigHashNone != SigHashNone {
		sigHash.Write(sigHashes.HashOutputs[:])
	} else if hashType&sigHashMask == SigHashSingle && idx < len(tx.TxOut) {
		var b bytes.Buffer
		wire.WriteTxOut(&b, 0, 0, tx.TxOut[idx])
		sigHash.Write(chainhash.DoubleHashB(b.Bytes()))
	} else {
		sigHash.Write(zeroHash[:])
	}

	// Finally, write out the transaction's locktime, and the sig hash
	// type.
	var bLockTime [4]byte
	binary.LittleEndian.PutUint32(bLockTime[:], tx.LockTime)
	sigHash.Write(bLockTime[:])
	var bHashType [4]byte
	binary.LittleEndian.PutUint32(bHashType[:], uint32(hashType))
	sigHash.Write(bHashType[:])

	return chainhash.DoubleHashRaw(sigHash), nil
}

// CalcWitnessSigHash computes the sighash digest for the specified input of
// the target transaction observing the desired sig hash type.
func CalcWitnessSigHash(script []byte, sigHashes *TxSigHashes, hType SigHashType,
	tx *wire.MsgTx, idx int, amt int64) ([]byte, error) {

	const scriptVersion = 0
	if err := checkScriptParses(scriptVersion, script); err != nil {
		return nil, err
	}

	return calcWitnessSignatureHashRaw(script, sigHashes, hType, tx, idx, amt)
}
