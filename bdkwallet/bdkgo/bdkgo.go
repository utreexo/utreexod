package bdkgo

// #include <bdkgo.h>
import "C"

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// This is needed, because as of go 1.24
// type RustBuffer C.RustBuffer cannot have methods,
// RustBuffer is treated as non-local type
type GoRustBuffer struct {
	inner C.RustBuffer
}

type RustBufferI interface {
	AsReader() *bytes.Reader
	Free()
	ToGoBytes() []byte
	Data() unsafe.Pointer
	Len() uint64
	Capacity() uint64
}

func RustBufferFromExternal(b RustBufferI) GoRustBuffer {
	return GoRustBuffer{
		inner: C.RustBuffer{
			capacity: C.uint64_t(b.Capacity()),
			len:      C.uint64_t(b.Len()),
			data:     (*C.uchar)(b.Data()),
		},
	}
}

func (cb GoRustBuffer) Capacity() uint64 {
	return uint64(cb.inner.capacity)
}

func (cb GoRustBuffer) Len() uint64 {
	return uint64(cb.inner.len)
}

func (cb GoRustBuffer) Data() unsafe.Pointer {
	return unsafe.Pointer(cb.inner.data)
}

func (cb GoRustBuffer) AsReader() *bytes.Reader {
	b := unsafe.Slice((*byte)(cb.inner.data), C.uint64_t(cb.inner.len))
	return bytes.NewReader(b)
}

func (cb GoRustBuffer) Free() {
	rustCall(func(status *C.RustCallStatus) bool {
		C.ffi_bdkgo_rustbuffer_free(cb.inner, status)
		return false
	})
}

func (cb GoRustBuffer) ToGoBytes() []byte {
	return C.GoBytes(unsafe.Pointer(cb.inner.data), C.int(cb.inner.len))
}

func stringToRustBuffer(str string) C.RustBuffer {
	return bytesToRustBuffer([]byte(str))
}

func bytesToRustBuffer(b []byte) C.RustBuffer {
	if len(b) == 0 {
		return C.RustBuffer{}
	}
	// We can pass the pointer along here, as it is pinned
	// for the duration of this call
	foreign := C.ForeignBytes{
		len:  C.int(len(b)),
		data: (*C.uchar)(unsafe.Pointer(&b[0])),
	}

	return rustCall(func(status *C.RustCallStatus) C.RustBuffer {
		return C.ffi_bdkgo_rustbuffer_from_bytes(foreign, status)
	})
}

type BufLifter[GoType any] interface {
	Lift(value RustBufferI) GoType
}

type BufLowerer[GoType any] interface {
	Lower(value GoType) C.RustBuffer
}

type BufReader[GoType any] interface {
	Read(reader io.Reader) GoType
}

type BufWriter[GoType any] interface {
	Write(writer io.Writer, value GoType)
}

func LowerIntoRustBuffer[GoType any](bufWriter BufWriter[GoType], value GoType) C.RustBuffer {
	// This might be not the most efficient way but it does not require knowing allocation size
	// beforehand
	var buffer bytes.Buffer
	bufWriter.Write(&buffer, value)

	bytes, err := io.ReadAll(&buffer)
	if err != nil {
		panic(fmt.Errorf("reading written data: %w", err))
	}
	return bytesToRustBuffer(bytes)
}

func LiftFromRustBuffer[GoType any](bufReader BufReader[GoType], rbuf RustBufferI) GoType {
	defer rbuf.Free()
	reader := rbuf.AsReader()
	item := bufReader.Read(reader)
	if reader.Len() > 0 {
		// TODO: Remove this
		leftover, _ := io.ReadAll(reader)
		panic(fmt.Errorf("Junk remaining in buffer after lifting: %s", string(leftover)))
	}
	return item
}

func rustCallWithError[E any, U any](converter BufReader[*E], callback func(*C.RustCallStatus) U) (U, *E) {
	var status C.RustCallStatus
	returnValue := callback(&status)
	err := checkCallStatus(converter, status)
	return returnValue, err
}

func checkCallStatus[E any](converter BufReader[*E], status C.RustCallStatus) *E {
	switch status.code {
	case 0:
		return nil
	case 1:
		return LiftFromRustBuffer(converter, GoRustBuffer{inner: status.errorBuf})
	case 2:
		// when the rust code sees a panic, it tries to construct a rustBuffer
		// with the message.  but if that code panics, then it just sends back
		// an empty buffer.
		if status.errorBuf.len > 0 {
			panic(fmt.Errorf("%s", FfiConverterStringINSTANCE.Lift(GoRustBuffer{inner: status.errorBuf})))
		} else {
			panic(fmt.Errorf("Rust panicked while handling Rust panic"))
		}
	default:
		panic(fmt.Errorf("unknown status code: %d", status.code))
	}
}

func checkCallStatusUnknown(status C.RustCallStatus) error {
	switch status.code {
	case 0:
		return nil
	case 1:
		panic(fmt.Errorf("function not returning an error returned an error"))
	case 2:
		// when the rust code sees a panic, it tries to construct a C.RustBuffer
		// with the message.  but if that code panics, then it just sends back
		// an empty buffer.
		if status.errorBuf.len > 0 {
			panic(fmt.Errorf("%s", FfiConverterStringINSTANCE.Lift(GoRustBuffer{
				inner: status.errorBuf,
			})))
		} else {
			panic(fmt.Errorf("Rust panicked while handling Rust panic"))
		}
	default:
		return fmt.Errorf("unknown status code: %d", status.code)
	}
}

func rustCall[U any](callback func(*C.RustCallStatus) U) U {
	returnValue, err := rustCallWithError[error](nil, callback)
	if err != nil {
		panic(err)
	}
	return returnValue
}

type NativeError interface {
	AsError() error
}

func writeInt8(writer io.Writer, value int8) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeUint8(writer io.Writer, value uint8) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeInt16(writer io.Writer, value int16) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeUint16(writer io.Writer, value uint16) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeInt32(writer io.Writer, value int32) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeUint32(writer io.Writer, value uint32) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeInt64(writer io.Writer, value int64) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeUint64(writer io.Writer, value uint64) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeFloat32(writer io.Writer, value float32) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeFloat64(writer io.Writer, value float64) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func readInt8(reader io.Reader) int8 {
	var result int8
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readUint8(reader io.Reader) uint8 {
	var result uint8
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readInt16(reader io.Reader) int16 {
	var result int16
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readUint16(reader io.Reader) uint16 {
	var result uint16
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readInt32(reader io.Reader) int32 {
	var result int32
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readUint32(reader io.Reader) uint32 {
	var result uint32
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readInt64(reader io.Reader) int64 {
	var result int64
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readUint64(reader io.Reader) uint64 {
	var result uint64
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readFloat32(reader io.Reader) float32 {
	var result float32
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readFloat64(reader io.Reader) float64 {
	var result float64
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func init() {

	uniffiCheckChecksums()
}

func uniffiCheckChecksums() {
	// Get the bindings contract version from our ComponentInterface
	bindingsContractVersion := 26
	// Get the scaffolding contract version by calling the into the dylib
	scaffoldingContractVersion := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint32_t {
		return C.ffi_bdkgo_uniffi_contract_version()
	})
	if bindingsContractVersion != int(scaffoldingContractVersion) {
		// If this happens try cleaning and rebuilding your project
		panic("bdkgo: UniFFI contract version mismatch")
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_apply_block()
		})
		if checksum != 5705 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_apply_block: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_apply_mempool()
		})
		if checksum != 24216 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_apply_mempool: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_balance()
		})
		if checksum != 30648 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_balance: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_create_tx()
		})
		if checksum != 56495 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_create_tx: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_fresh_address()
		})
		if checksum != 25662 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_fresh_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_genesis_hash()
		})
		if checksum != 65013 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_genesis_hash: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_increment_reference_counter()
		})
		if checksum != 61284 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_increment_reference_counter: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_last_unused_address()
		})
		if checksum != 55250 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_last_unused_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_mnemonic_words()
		})
		if checksum != 29291 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_mnemonic_words: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_peek_address()
		})
		if checksum != 11815 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_peek_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_recent_blocks()
		})
		if checksum != 34204 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_recent_blocks: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_transactions()
		})
		if checksum != 1772 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_transactions: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_utxos()
		})
		if checksum != 16415 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_utxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_constructor_wallet_create_new()
		})
		if checksum != 8274 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_constructor_wallet_create_new: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_constructor_wallet_load()
		})
		if checksum != 20250 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_constructor_wallet_load: UniFFI API checksum mismatch")
		}
	}
}

type FfiConverterUint32 struct{}

var FfiConverterUint32INSTANCE = FfiConverterUint32{}

func (FfiConverterUint32) Lower(value uint32) C.uint32_t {
	return C.uint32_t(value)
}

func (FfiConverterUint32) Write(writer io.Writer, value uint32) {
	writeUint32(writer, value)
}

func (FfiConverterUint32) Lift(value C.uint32_t) uint32 {
	return uint32(value)
}

func (FfiConverterUint32) Read(reader io.Reader) uint32 {
	return readUint32(reader)
}

type FfiDestroyerUint32 struct{}

func (FfiDestroyerUint32) Destroy(_ uint32) {}

type FfiConverterUint64 struct{}

var FfiConverterUint64INSTANCE = FfiConverterUint64{}

func (FfiConverterUint64) Lower(value uint64) C.uint64_t {
	return C.uint64_t(value)
}

func (FfiConverterUint64) Write(writer io.Writer, value uint64) {
	writeUint64(writer, value)
}

func (FfiConverterUint64) Lift(value C.uint64_t) uint64 {
	return uint64(value)
}

func (FfiConverterUint64) Read(reader io.Reader) uint64 {
	return readUint64(reader)
}

type FfiDestroyerUint64 struct{}

func (FfiDestroyerUint64) Destroy(_ uint64) {}

type FfiConverterBool struct{}

var FfiConverterBoolINSTANCE = FfiConverterBool{}

func (FfiConverterBool) Lower(value bool) C.int8_t {
	if value {
		return C.int8_t(1)
	}
	return C.int8_t(0)
}

func (FfiConverterBool) Write(writer io.Writer, value bool) {
	if value {
		writeInt8(writer, 1)
	} else {
		writeInt8(writer, 0)
	}
}

func (FfiConverterBool) Lift(value C.int8_t) bool {
	return value != 0
}

func (FfiConverterBool) Read(reader io.Reader) bool {
	return readInt8(reader) != 0
}

type FfiDestroyerBool struct{}

func (FfiDestroyerBool) Destroy(_ bool) {}

type FfiConverterString struct{}

var FfiConverterStringINSTANCE = FfiConverterString{}

func (FfiConverterString) Lift(rb RustBufferI) string {
	defer rb.Free()
	reader := rb.AsReader()
	b, err := io.ReadAll(reader)
	if err != nil {
		panic(fmt.Errorf("reading reader: %w", err))
	}
	return string(b)
}

func (FfiConverterString) Read(reader io.Reader) string {
	length := readInt32(reader)
	buffer := make([]byte, length)
	read_length, err := reader.Read(buffer)
	if err != nil && err != io.EOF {
		panic(err)
	}
	if read_length != int(length) {
		panic(fmt.Errorf("bad read length when reading string, expected %d, read %d", length, read_length))
	}
	return string(buffer)
}

func (FfiConverterString) Lower(value string) C.RustBuffer {
	return stringToRustBuffer(value)
}

func (FfiConverterString) Write(writer io.Writer, value string) {
	if len(value) > math.MaxInt32 {
		panic("String is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	write_length, err := io.WriteString(writer, value)
	if err != nil {
		panic(err)
	}
	if write_length != len(value) {
		panic(fmt.Errorf("bad write length when writing string, expected %d, written %d", len(value), write_length))
	}
}

type FfiDestroyerString struct{}

func (FfiDestroyerString) Destroy(_ string) {}

type FfiConverterBytes struct{}

var FfiConverterBytesINSTANCE = FfiConverterBytes{}

func (c FfiConverterBytes) Lower(value []byte) C.RustBuffer {
	return LowerIntoRustBuffer[[]byte](c, value)
}

func (c FfiConverterBytes) Write(writer io.Writer, value []byte) {
	if len(value) > math.MaxInt32 {
		panic("[]byte is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	write_length, err := writer.Write(value)
	if err != nil {
		panic(err)
	}
	if write_length != len(value) {
		panic(fmt.Errorf("bad write length when writing []byte, expected %d, written %d", len(value), write_length))
	}
}

func (c FfiConverterBytes) Lift(rb RustBufferI) []byte {
	return LiftFromRustBuffer[[]byte](c, rb)
}

func (c FfiConverterBytes) Read(reader io.Reader) []byte {
	length := readInt32(reader)
	buffer := make([]byte, length)
	read_length, err := reader.Read(buffer)
	if err != nil && err != io.EOF {
		panic(err)
	}
	if read_length != int(length) {
		panic(fmt.Errorf("bad read length when reading []byte, expected %d, read %d", length, read_length))
	}
	return buffer
}

type FfiDestroyerBytes struct{}

func (FfiDestroyerBytes) Destroy(_ []byte) {}

// Below is an implementation of synchronization requirements outlined in the link.
// https://github.com/mozilla/uniffi-rs/blob/0dc031132d9493ca812c3af6e7dd60ad2ea95bf0/uniffi_bindgen/src/bindings/kotlin/templates/ObjectRuntime.kt#L31

type FfiObject struct {
	pointer       unsafe.Pointer
	callCounter   atomic.Int64
	cloneFunction func(unsafe.Pointer, *C.RustCallStatus) unsafe.Pointer
	freeFunction  func(unsafe.Pointer, *C.RustCallStatus)
	destroyed     atomic.Bool
}

func newFfiObject(
	pointer unsafe.Pointer,
	cloneFunction func(unsafe.Pointer, *C.RustCallStatus) unsafe.Pointer,
	freeFunction func(unsafe.Pointer, *C.RustCallStatus),
) FfiObject {
	return FfiObject{
		pointer:       pointer,
		cloneFunction: cloneFunction,
		freeFunction:  freeFunction,
	}
}

func (ffiObject *FfiObject) incrementPointer(debugName string) unsafe.Pointer {
	for {
		counter := ffiObject.callCounter.Load()
		if counter <= -1 {
			panic(fmt.Errorf("%v object has already been destroyed", debugName))
		}
		if counter == math.MaxInt64 {
			panic(fmt.Errorf("%v object call counter would overflow", debugName))
		}
		if ffiObject.callCounter.CompareAndSwap(counter, counter+1) {
			break
		}
	}

	return rustCall(func(status *C.RustCallStatus) unsafe.Pointer {
		return ffiObject.cloneFunction(ffiObject.pointer, status)
	})
}

func (ffiObject *FfiObject) decrementPointer() {
	if ffiObject.callCounter.Add(-1) == -1 {
		ffiObject.freeRustArcPtr()
	}
}

func (ffiObject *FfiObject) destroy() {
	if ffiObject.destroyed.CompareAndSwap(false, true) {
		if ffiObject.callCounter.Add(-1) == -1 {
			ffiObject.freeRustArcPtr()
		}
	}
}

func (ffiObject *FfiObject) freeRustArcPtr() {
	rustCall(func(status *C.RustCallStatus) int32 {
		ffiObject.freeFunction(ffiObject.pointer, status)
		return 0
	})
}

type WalletInterface interface {
	ApplyBlock(height uint32, blockBytes []byte) (ApplyResult, error)
	ApplyMempool(txs []MempoolTx) (ApplyResult, error)
	Balance() Balance
	CreateTx(feerate uint64, recipients []Recipient) ([]byte, error)
	FreshAddress() (AddressInfo, error)
	GenesisHash() []byte
	IncrementReferenceCounter()
	LastUnusedAddress() (AddressInfo, error)
	MnemonicWords() []string
	PeekAddress(index uint32) (AddressInfo, error)
	RecentBlocks(count uint32) []BlockId
	Transactions() []TxInfo
	Utxos() []UtxoInfo
}
type Wallet struct {
	ffiObject FfiObject
}

func WalletCreateNew(dbPath string, network string) (*Wallet, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[CreateNewError](FfiConverterCreateNewError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_bdkgo_fn_constructor_wallet_create_new(FfiConverterStringINSTANCE.Lower(dbPath), FfiConverterStringINSTANCE.Lower(network), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *Wallet
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterWalletINSTANCE.Lift(_uniffiRV), nil
	}
}

func WalletLoad(dbPath string, genesisHash []byte) (*Wallet, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[LoadError](FfiConverterLoadError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_bdkgo_fn_constructor_wallet_load(FfiConverterStringINSTANCE.Lower(dbPath), FfiConverterBytesINSTANCE.Lower(genesisHash), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *Wallet
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterWalletINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *Wallet) ApplyBlock(height uint32, blockBytes []byte) (ApplyResult, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[ApplyBlockError](FfiConverterApplyBlockError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bdkgo_fn_method_wallet_apply_block(
				_pointer, FfiConverterUint32INSTANCE.Lower(height), FfiConverterBytesINSTANCE.Lower(blockBytes), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue ApplyResult
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterApplyResultINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *Wallet) ApplyMempool(txs []MempoolTx) (ApplyResult, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[ApplyMempoolError](FfiConverterApplyMempoolError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bdkgo_fn_method_wallet_apply_mempool(
				_pointer, FfiConverterSequenceMempoolTxINSTANCE.Lower(txs), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue ApplyResult
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterApplyResultINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *Wallet) Balance() Balance {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBalanceINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bdkgo_fn_method_wallet_balance(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Wallet) CreateTx(feerate uint64, recipients []Recipient) ([]byte, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[CreateTxError](FfiConverterCreateTxError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bdkgo_fn_method_wallet_create_tx(
				_pointer, FfiConverterUint64INSTANCE.Lower(feerate), FfiConverterSequenceRecipientINSTANCE.Lower(recipients), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue []byte
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterBytesINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *Wallet) FreshAddress() (AddressInfo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[DatabaseError](FfiConverterDatabaseError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bdkgo_fn_method_wallet_fresh_address(
				_pointer, _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue AddressInfo
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterAddressInfoINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *Wallet) GenesisHash() []byte {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bdkgo_fn_method_wallet_genesis_hash(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Wallet) IncrementReferenceCounter() {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	rustCall(func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_bdkgo_fn_method_wallet_increment_reference_counter(
			_pointer, _uniffiStatus)
		return false
	})
}

func (_self *Wallet) LastUnusedAddress() (AddressInfo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[DatabaseError](FfiConverterDatabaseError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bdkgo_fn_method_wallet_last_unused_address(
				_pointer, _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue AddressInfo
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterAddressInfoINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *Wallet) MnemonicWords() []string {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterSequenceStringINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bdkgo_fn_method_wallet_mnemonic_words(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Wallet) PeekAddress(index uint32) (AddressInfo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[DatabaseError](FfiConverterDatabaseError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bdkgo_fn_method_wallet_peek_address(
				_pointer, FfiConverterUint32INSTANCE.Lower(index), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue AddressInfo
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterAddressInfoINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *Wallet) RecentBlocks(count uint32) []BlockId {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterSequenceBlockIdINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bdkgo_fn_method_wallet_recent_blocks(
				_pointer, FfiConverterUint32INSTANCE.Lower(count), _uniffiStatus),
		}
	}))
}

func (_self *Wallet) Transactions() []TxInfo {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterSequenceTxInfoINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bdkgo_fn_method_wallet_transactions(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Wallet) Utxos() []UtxoInfo {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterSequenceUtxoInfoINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_bdkgo_fn_method_wallet_utxos(
				_pointer, _uniffiStatus),
		}
	}))
}
func (object *Wallet) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterWallet struct{}

var FfiConverterWalletINSTANCE = FfiConverterWallet{}

func (c FfiConverterWallet) Lift(pointer unsafe.Pointer) *Wallet {
	result := &Wallet{
		newFfiObject(
			pointer,
			func(pointer unsafe.Pointer, status *C.RustCallStatus) unsafe.Pointer {
				return C.uniffi_bdkgo_fn_clone_wallet(pointer, status)
			},
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_bdkgo_fn_free_wallet(pointer, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*Wallet).Destroy)
	return result
}

func (c FfiConverterWallet) Read(reader io.Reader) *Wallet {
	return c.Lift(unsafe.Pointer(uintptr(readUint64(reader))))
}

func (c FfiConverterWallet) Lower(value *Wallet) unsafe.Pointer {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the pointer will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked pointer.
	pointer := value.ffiObject.incrementPointer("*Wallet")
	defer value.ffiObject.decrementPointer()
	return pointer

}

func (c FfiConverterWallet) Write(writer io.Writer, value *Wallet) {
	writeUint64(writer, uint64(uintptr(c.Lower(value))))
}

type FfiDestroyerWallet struct{}

func (_ FfiDestroyerWallet) Destroy(value *Wallet) {
	value.Destroy()
}

type AddressInfo struct {
	Index   uint32
	Address string
}

func (r *AddressInfo) Destroy() {
	FfiDestroyerUint32{}.Destroy(r.Index)
	FfiDestroyerString{}.Destroy(r.Address)
}

type FfiConverterAddressInfo struct{}

var FfiConverterAddressInfoINSTANCE = FfiConverterAddressInfo{}

func (c FfiConverterAddressInfo) Lift(rb RustBufferI) AddressInfo {
	return LiftFromRustBuffer[AddressInfo](c, rb)
}

func (c FfiConverterAddressInfo) Read(reader io.Reader) AddressInfo {
	return AddressInfo{
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
	}
}

func (c FfiConverterAddressInfo) Lower(value AddressInfo) C.RustBuffer {
	return LowerIntoRustBuffer[AddressInfo](c, value)
}

func (c FfiConverterAddressInfo) Write(writer io.Writer, value AddressInfo) {
	FfiConverterUint32INSTANCE.Write(writer, value.Index)
	FfiConverterStringINSTANCE.Write(writer, value.Address)
}

type FfiDestroyerAddressInfo struct{}

func (_ FfiDestroyerAddressInfo) Destroy(value AddressInfo) {
	value.Destroy()
}

type ApplyResult struct {
	RelevantTxids [][]byte
}

func (r *ApplyResult) Destroy() {
	FfiDestroyerSequenceBytes{}.Destroy(r.RelevantTxids)
}

type FfiConverterApplyResult struct{}

var FfiConverterApplyResultINSTANCE = FfiConverterApplyResult{}

func (c FfiConverterApplyResult) Lift(rb RustBufferI) ApplyResult {
	return LiftFromRustBuffer[ApplyResult](c, rb)
}

func (c FfiConverterApplyResult) Read(reader io.Reader) ApplyResult {
	return ApplyResult{
		FfiConverterSequenceBytesINSTANCE.Read(reader),
	}
}

func (c FfiConverterApplyResult) Lower(value ApplyResult) C.RustBuffer {
	return LowerIntoRustBuffer[ApplyResult](c, value)
}

func (c FfiConverterApplyResult) Write(writer io.Writer, value ApplyResult) {
	FfiConverterSequenceBytesINSTANCE.Write(writer, value.RelevantTxids)
}

type FfiDestroyerApplyResult struct{}

func (_ FfiDestroyerApplyResult) Destroy(value ApplyResult) {
	value.Destroy()
}

type Balance struct {
	Immature         uint64
	TrustedPending   uint64
	UntrustedPending uint64
	Confirmed        uint64
}

func (r *Balance) Destroy() {
	FfiDestroyerUint64{}.Destroy(r.Immature)
	FfiDestroyerUint64{}.Destroy(r.TrustedPending)
	FfiDestroyerUint64{}.Destroy(r.UntrustedPending)
	FfiDestroyerUint64{}.Destroy(r.Confirmed)
}

type FfiConverterBalance struct{}

var FfiConverterBalanceINSTANCE = FfiConverterBalance{}

func (c FfiConverterBalance) Lift(rb RustBufferI) Balance {
	return LiftFromRustBuffer[Balance](c, rb)
}

func (c FfiConverterBalance) Read(reader io.Reader) Balance {
	return Balance{
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterBalance) Lower(value Balance) C.RustBuffer {
	return LowerIntoRustBuffer[Balance](c, value)
}

func (c FfiConverterBalance) Write(writer io.Writer, value Balance) {
	FfiConverterUint64INSTANCE.Write(writer, value.Immature)
	FfiConverterUint64INSTANCE.Write(writer, value.TrustedPending)
	FfiConverterUint64INSTANCE.Write(writer, value.UntrustedPending)
	FfiConverterUint64INSTANCE.Write(writer, value.Confirmed)
}

type FfiDestroyerBalance struct{}

func (_ FfiDestroyerBalance) Destroy(value Balance) {
	value.Destroy()
}

type BlockId struct {
	Height uint32
	Hash   []byte
}

func (r *BlockId) Destroy() {
	FfiDestroyerUint32{}.Destroy(r.Height)
	FfiDestroyerBytes{}.Destroy(r.Hash)
}

type FfiConverterBlockId struct{}

var FfiConverterBlockIdINSTANCE = FfiConverterBlockId{}

func (c FfiConverterBlockId) Lift(rb RustBufferI) BlockId {
	return LiftFromRustBuffer[BlockId](c, rb)
}

func (c FfiConverterBlockId) Read(reader io.Reader) BlockId {
	return BlockId{
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterBytesINSTANCE.Read(reader),
	}
}

func (c FfiConverterBlockId) Lower(value BlockId) C.RustBuffer {
	return LowerIntoRustBuffer[BlockId](c, value)
}

func (c FfiConverterBlockId) Write(writer io.Writer, value BlockId) {
	FfiConverterUint32INSTANCE.Write(writer, value.Height)
	FfiConverterBytesINSTANCE.Write(writer, value.Hash)
}

type FfiDestroyerBlockId struct{}

func (_ FfiDestroyerBlockId) Destroy(value BlockId) {
	value.Destroy()
}

type MempoolTx struct {
	Tx        []byte
	AddedUnix uint64
}

func (r *MempoolTx) Destroy() {
	FfiDestroyerBytes{}.Destroy(r.Tx)
	FfiDestroyerUint64{}.Destroy(r.AddedUnix)
}

type FfiConverterMempoolTx struct{}

var FfiConverterMempoolTxINSTANCE = FfiConverterMempoolTx{}

func (c FfiConverterMempoolTx) Lift(rb RustBufferI) MempoolTx {
	return LiftFromRustBuffer[MempoolTx](c, rb)
}

func (c FfiConverterMempoolTx) Read(reader io.Reader) MempoolTx {
	return MempoolTx{
		FfiConverterBytesINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterMempoolTx) Lower(value MempoolTx) C.RustBuffer {
	return LowerIntoRustBuffer[MempoolTx](c, value)
}

func (c FfiConverterMempoolTx) Write(writer io.Writer, value MempoolTx) {
	FfiConverterBytesINSTANCE.Write(writer, value.Tx)
	FfiConverterUint64INSTANCE.Write(writer, value.AddedUnix)
}

type FfiDestroyerMempoolTx struct{}

func (_ FfiDestroyerMempoolTx) Destroy(value MempoolTx) {
	value.Destroy()
}

type Recipient struct {
	Address string
	Amount  uint64
}

func (r *Recipient) Destroy() {
	FfiDestroyerString{}.Destroy(r.Address)
	FfiDestroyerUint64{}.Destroy(r.Amount)
}

type FfiConverterRecipient struct{}

var FfiConverterRecipientINSTANCE = FfiConverterRecipient{}

func (c FfiConverterRecipient) Lift(rb RustBufferI) Recipient {
	return LiftFromRustBuffer[Recipient](c, rb)
}

func (c FfiConverterRecipient) Read(reader io.Reader) Recipient {
	return Recipient{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterRecipient) Lower(value Recipient) C.RustBuffer {
	return LowerIntoRustBuffer[Recipient](c, value)
}

func (c FfiConverterRecipient) Write(writer io.Writer, value Recipient) {
	FfiConverterStringINSTANCE.Write(writer, value.Address)
	FfiConverterUint64INSTANCE.Write(writer, value.Amount)
}

type FfiDestroyerRecipient struct{}

func (_ FfiDestroyerRecipient) Destroy(value Recipient) {
	value.Destroy()
}

type TxInfo struct {
	Txid          []byte
	Tx            []byte
	Spent         uint64
	Received      uint64
	Confirmations uint32
}

func (r *TxInfo) Destroy() {
	FfiDestroyerBytes{}.Destroy(r.Txid)
	FfiDestroyerBytes{}.Destroy(r.Tx)
	FfiDestroyerUint64{}.Destroy(r.Spent)
	FfiDestroyerUint64{}.Destroy(r.Received)
	FfiDestroyerUint32{}.Destroy(r.Confirmations)
}

type FfiConverterTxInfo struct{}

var FfiConverterTxInfoINSTANCE = FfiConverterTxInfo{}

func (c FfiConverterTxInfo) Lift(rb RustBufferI) TxInfo {
	return LiftFromRustBuffer[TxInfo](c, rb)
}

func (c FfiConverterTxInfo) Read(reader io.Reader) TxInfo {
	return TxInfo{
		FfiConverterBytesINSTANCE.Read(reader),
		FfiConverterBytesINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
	}
}

func (c FfiConverterTxInfo) Lower(value TxInfo) C.RustBuffer {
	return LowerIntoRustBuffer[TxInfo](c, value)
}

func (c FfiConverterTxInfo) Write(writer io.Writer, value TxInfo) {
	FfiConverterBytesINSTANCE.Write(writer, value.Txid)
	FfiConverterBytesINSTANCE.Write(writer, value.Tx)
	FfiConverterUint64INSTANCE.Write(writer, value.Spent)
	FfiConverterUint64INSTANCE.Write(writer, value.Received)
	FfiConverterUint32INSTANCE.Write(writer, value.Confirmations)
}

type FfiDestroyerTxInfo struct{}

func (_ FfiDestroyerTxInfo) Destroy(value TxInfo) {
	value.Destroy()
}

type UtxoInfo struct {
	Txid            []byte
	Vout            uint32
	Amount          uint64
	ScriptPubkey    []byte
	IsChange        bool
	DerivationIndex uint32
	Confirmations   uint32
}

func (r *UtxoInfo) Destroy() {
	FfiDestroyerBytes{}.Destroy(r.Txid)
	FfiDestroyerUint32{}.Destroy(r.Vout)
	FfiDestroyerUint64{}.Destroy(r.Amount)
	FfiDestroyerBytes{}.Destroy(r.ScriptPubkey)
	FfiDestroyerBool{}.Destroy(r.IsChange)
	FfiDestroyerUint32{}.Destroy(r.DerivationIndex)
	FfiDestroyerUint32{}.Destroy(r.Confirmations)
}

type FfiConverterUtxoInfo struct{}

var FfiConverterUtxoInfoINSTANCE = FfiConverterUtxoInfo{}

func (c FfiConverterUtxoInfo) Lift(rb RustBufferI) UtxoInfo {
	return LiftFromRustBuffer[UtxoInfo](c, rb)
}

func (c FfiConverterUtxoInfo) Read(reader io.Reader) UtxoInfo {
	return UtxoInfo{
		FfiConverterBytesINSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterBytesINSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
	}
}

func (c FfiConverterUtxoInfo) Lower(value UtxoInfo) C.RustBuffer {
	return LowerIntoRustBuffer[UtxoInfo](c, value)
}

func (c FfiConverterUtxoInfo) Write(writer io.Writer, value UtxoInfo) {
	FfiConverterBytesINSTANCE.Write(writer, value.Txid)
	FfiConverterUint32INSTANCE.Write(writer, value.Vout)
	FfiConverterUint64INSTANCE.Write(writer, value.Amount)
	FfiConverterBytesINSTANCE.Write(writer, value.ScriptPubkey)
	FfiConverterBoolINSTANCE.Write(writer, value.IsChange)
	FfiConverterUint32INSTANCE.Write(writer, value.DerivationIndex)
	FfiConverterUint32INSTANCE.Write(writer, value.Confirmations)
}

type FfiDestroyerUtxoInfo struct{}

func (_ FfiDestroyerUtxoInfo) Destroy(value UtxoInfo) {
	value.Destroy()
}

type ApplyBlockError struct {
	err error
}

// Convience method to turn *ApplyBlockError into error
// Avoiding treating nil pointer as non nil error interface
func (err *ApplyBlockError) AsError() error {
	if err == nil {
		return nil
	} else {
		return err
	}
}

func (err ApplyBlockError) Error() string {
	return fmt.Sprintf("ApplyBlockError: %s", err.err.Error())
}

func (err ApplyBlockError) Unwrap() error {
	return err.err
}

// Err* are used for checking error type with `errors.Is`
var ErrApplyBlockErrorDecodeBlock = fmt.Errorf("ApplyBlockErrorDecodeBlock")
var ErrApplyBlockErrorCannotConnect = fmt.Errorf("ApplyBlockErrorCannotConnect")
var ErrApplyBlockErrorDatabase = fmt.Errorf("ApplyBlockErrorDatabase")

// Variant structs
type ApplyBlockErrorDecodeBlock struct {
	message string
}

func NewApplyBlockErrorDecodeBlock() *ApplyBlockError {
	return &ApplyBlockError{err: &ApplyBlockErrorDecodeBlock{}}
}

func (e ApplyBlockErrorDecodeBlock) destroy() {
}

func (err ApplyBlockErrorDecodeBlock) Error() string {
	return fmt.Sprintf("DecodeBlock: %s", err.message)
}

func (self ApplyBlockErrorDecodeBlock) Is(target error) bool {
	return target == ErrApplyBlockErrorDecodeBlock
}

type ApplyBlockErrorCannotConnect struct {
	message string
}

func NewApplyBlockErrorCannotConnect() *ApplyBlockError {
	return &ApplyBlockError{err: &ApplyBlockErrorCannotConnect{}}
}

func (e ApplyBlockErrorCannotConnect) destroy() {
}

func (err ApplyBlockErrorCannotConnect) Error() string {
	return fmt.Sprintf("CannotConnect: %s", err.message)
}

func (self ApplyBlockErrorCannotConnect) Is(target error) bool {
	return target == ErrApplyBlockErrorCannotConnect
}

type ApplyBlockErrorDatabase struct {
	message string
}

func NewApplyBlockErrorDatabase() *ApplyBlockError {
	return &ApplyBlockError{err: &ApplyBlockErrorDatabase{}}
}

func (e ApplyBlockErrorDatabase) destroy() {
}

func (err ApplyBlockErrorDatabase) Error() string {
	return fmt.Sprintf("Database: %s", err.message)
}

func (self ApplyBlockErrorDatabase) Is(target error) bool {
	return target == ErrApplyBlockErrorDatabase
}

type FfiConverterApplyBlockError struct{}

var FfiConverterApplyBlockErrorINSTANCE = FfiConverterApplyBlockError{}

func (c FfiConverterApplyBlockError) Lift(eb RustBufferI) *ApplyBlockError {
	return LiftFromRustBuffer[*ApplyBlockError](c, eb)
}

func (c FfiConverterApplyBlockError) Lower(value *ApplyBlockError) C.RustBuffer {
	return LowerIntoRustBuffer[*ApplyBlockError](c, value)
}

func (c FfiConverterApplyBlockError) Read(reader io.Reader) *ApplyBlockError {
	errorID := readUint32(reader)

	message := FfiConverterStringINSTANCE.Read(reader)
	switch errorID {
	case 1:
		return &ApplyBlockError{&ApplyBlockErrorDecodeBlock{message}}
	case 2:
		return &ApplyBlockError{&ApplyBlockErrorCannotConnect{message}}
	case 3:
		return &ApplyBlockError{&ApplyBlockErrorDatabase{message}}
	default:
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterApplyBlockError.Read()", errorID))
	}

}

func (c FfiConverterApplyBlockError) Write(writer io.Writer, value *ApplyBlockError) {
	switch variantValue := value.err.(type) {
	case *ApplyBlockErrorDecodeBlock:
		writeInt32(writer, 1)
	case *ApplyBlockErrorCannotConnect:
		writeInt32(writer, 2)
	case *ApplyBlockErrorDatabase:
		writeInt32(writer, 3)
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiConverterApplyBlockError.Write", value))
	}
}

type FfiDestroyerApplyBlockError struct{}

func (_ FfiDestroyerApplyBlockError) Destroy(value *ApplyBlockError) {
	switch variantValue := value.err.(type) {
	case ApplyBlockErrorDecodeBlock:
		variantValue.destroy()
	case ApplyBlockErrorCannotConnect:
		variantValue.destroy()
	case ApplyBlockErrorDatabase:
		variantValue.destroy()
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiDestroyerApplyBlockError.Destroy", value))
	}
}

type ApplyMempoolError struct {
	err error
}

// Convience method to turn *ApplyMempoolError into error
// Avoiding treating nil pointer as non nil error interface
func (err *ApplyMempoolError) AsError() error {
	if err == nil {
		return nil
	} else {
		return err
	}
}

func (err ApplyMempoolError) Error() string {
	return fmt.Sprintf("ApplyMempoolError: %s", err.err.Error())
}

func (err ApplyMempoolError) Unwrap() error {
	return err.err
}

// Err* are used for checking error type with `errors.Is`
var ErrApplyMempoolErrorDatabase = fmt.Errorf("ApplyMempoolErrorDatabase")

// Variant structs
type ApplyMempoolErrorDatabase struct {
	message string
}

func NewApplyMempoolErrorDatabase() *ApplyMempoolError {
	return &ApplyMempoolError{err: &ApplyMempoolErrorDatabase{}}
}

func (e ApplyMempoolErrorDatabase) destroy() {
}

func (err ApplyMempoolErrorDatabase) Error() string {
	return fmt.Sprintf("Database: %s", err.message)
}

func (self ApplyMempoolErrorDatabase) Is(target error) bool {
	return target == ErrApplyMempoolErrorDatabase
}

type FfiConverterApplyMempoolError struct{}

var FfiConverterApplyMempoolErrorINSTANCE = FfiConverterApplyMempoolError{}

func (c FfiConverterApplyMempoolError) Lift(eb RustBufferI) *ApplyMempoolError {
	return LiftFromRustBuffer[*ApplyMempoolError](c, eb)
}

func (c FfiConverterApplyMempoolError) Lower(value *ApplyMempoolError) C.RustBuffer {
	return LowerIntoRustBuffer[*ApplyMempoolError](c, value)
}

func (c FfiConverterApplyMempoolError) Read(reader io.Reader) *ApplyMempoolError {
	errorID := readUint32(reader)

	message := FfiConverterStringINSTANCE.Read(reader)
	switch errorID {
	case 1:
		return &ApplyMempoolError{&ApplyMempoolErrorDatabase{message}}
	default:
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterApplyMempoolError.Read()", errorID))
	}

}

func (c FfiConverterApplyMempoolError) Write(writer io.Writer, value *ApplyMempoolError) {
	switch variantValue := value.err.(type) {
	case *ApplyMempoolErrorDatabase:
		writeInt32(writer, 1)
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiConverterApplyMempoolError.Write", value))
	}
}

type FfiDestroyerApplyMempoolError struct{}

func (_ FfiDestroyerApplyMempoolError) Destroy(value *ApplyMempoolError) {
	switch variantValue := value.err.(type) {
	case ApplyMempoolErrorDatabase:
		variantValue.destroy()
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiDestroyerApplyMempoolError.Destroy", value))
	}
}

type CreateNewError struct {
	err error
}

// Convience method to turn *CreateNewError into error
// Avoiding treating nil pointer as non nil error interface
func (err *CreateNewError) AsError() error {
	if err == nil {
		return nil
	} else {
		return err
	}
}

func (err CreateNewError) Error() string {
	return fmt.Sprintf("CreateNewError: %s", err.err.Error())
}

func (err CreateNewError) Unwrap() error {
	return err.err
}

// Err* are used for checking error type with `errors.Is`
var ErrCreateNewErrorParseNetwork = fmt.Errorf("CreateNewErrorParseNetwork")
var ErrCreateNewErrorParseGenesisHash = fmt.Errorf("CreateNewErrorParseGenesisHash")
var ErrCreateNewErrorDatabase = fmt.Errorf("CreateNewErrorDatabase")
var ErrCreateNewErrorWallet = fmt.Errorf("CreateNewErrorWallet")

// Variant structs
type CreateNewErrorParseNetwork struct {
	message string
}

func NewCreateNewErrorParseNetwork() *CreateNewError {
	return &CreateNewError{err: &CreateNewErrorParseNetwork{}}
}

func (e CreateNewErrorParseNetwork) destroy() {
}

func (err CreateNewErrorParseNetwork) Error() string {
	return fmt.Sprintf("ParseNetwork: %s", err.message)
}

func (self CreateNewErrorParseNetwork) Is(target error) bool {
	return target == ErrCreateNewErrorParseNetwork
}

type CreateNewErrorParseGenesisHash struct {
	message string
}

func NewCreateNewErrorParseGenesisHash() *CreateNewError {
	return &CreateNewError{err: &CreateNewErrorParseGenesisHash{}}
}

func (e CreateNewErrorParseGenesisHash) destroy() {
}

func (err CreateNewErrorParseGenesisHash) Error() string {
	return fmt.Sprintf("ParseGenesisHash: %s", err.message)
}

func (self CreateNewErrorParseGenesisHash) Is(target error) bool {
	return target == ErrCreateNewErrorParseGenesisHash
}

type CreateNewErrorDatabase struct {
	message string
}

func NewCreateNewErrorDatabase() *CreateNewError {
	return &CreateNewError{err: &CreateNewErrorDatabase{}}
}

func (e CreateNewErrorDatabase) destroy() {
}

func (err CreateNewErrorDatabase) Error() string {
	return fmt.Sprintf("Database: %s", err.message)
}

func (self CreateNewErrorDatabase) Is(target error) bool {
	return target == ErrCreateNewErrorDatabase
}

type CreateNewErrorWallet struct {
	message string
}

func NewCreateNewErrorWallet() *CreateNewError {
	return &CreateNewError{err: &CreateNewErrorWallet{}}
}

func (e CreateNewErrorWallet) destroy() {
}

func (err CreateNewErrorWallet) Error() string {
	return fmt.Sprintf("Wallet: %s", err.message)
}

func (self CreateNewErrorWallet) Is(target error) bool {
	return target == ErrCreateNewErrorWallet
}

type FfiConverterCreateNewError struct{}

var FfiConverterCreateNewErrorINSTANCE = FfiConverterCreateNewError{}

func (c FfiConverterCreateNewError) Lift(eb RustBufferI) *CreateNewError {
	return LiftFromRustBuffer[*CreateNewError](c, eb)
}

func (c FfiConverterCreateNewError) Lower(value *CreateNewError) C.RustBuffer {
	return LowerIntoRustBuffer[*CreateNewError](c, value)
}

func (c FfiConverterCreateNewError) Read(reader io.Reader) *CreateNewError {
	errorID := readUint32(reader)

	message := FfiConverterStringINSTANCE.Read(reader)
	switch errorID {
	case 1:
		return &CreateNewError{&CreateNewErrorParseNetwork{message}}
	case 2:
		return &CreateNewError{&CreateNewErrorParseGenesisHash{message}}
	case 3:
		return &CreateNewError{&CreateNewErrorDatabase{message}}
	case 4:
		return &CreateNewError{&CreateNewErrorWallet{message}}
	default:
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterCreateNewError.Read()", errorID))
	}

}

func (c FfiConverterCreateNewError) Write(writer io.Writer, value *CreateNewError) {
	switch variantValue := value.err.(type) {
	case *CreateNewErrorParseNetwork:
		writeInt32(writer, 1)
	case *CreateNewErrorParseGenesisHash:
		writeInt32(writer, 2)
	case *CreateNewErrorDatabase:
		writeInt32(writer, 3)
	case *CreateNewErrorWallet:
		writeInt32(writer, 4)
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiConverterCreateNewError.Write", value))
	}
}

type FfiDestroyerCreateNewError struct{}

func (_ FfiDestroyerCreateNewError) Destroy(value *CreateNewError) {
	switch variantValue := value.err.(type) {
	case CreateNewErrorParseNetwork:
		variantValue.destroy()
	case CreateNewErrorParseGenesisHash:
		variantValue.destroy()
	case CreateNewErrorDatabase:
		variantValue.destroy()
	case CreateNewErrorWallet:
		variantValue.destroy()
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiDestroyerCreateNewError.Destroy", value))
	}
}

type CreateTxError struct {
	err error
}

// Convience method to turn *CreateTxError into error
// Avoiding treating nil pointer as non nil error interface
func (err *CreateTxError) AsError() error {
	if err == nil {
		return nil
	} else {
		return err
	}
}

func (err CreateTxError) Error() string {
	return fmt.Sprintf("CreateTxError: %s", err.err.Error())
}

func (err CreateTxError) Unwrap() error {
	return err.err
}

// Err* are used for checking error type with `errors.Is`
var ErrCreateTxErrorInvalidAddress = fmt.Errorf("CreateTxErrorInvalidAddress")
var ErrCreateTxErrorCreateTx = fmt.Errorf("CreateTxErrorCreateTx")
var ErrCreateTxErrorSignTx = fmt.Errorf("CreateTxErrorSignTx")

// Variant structs
type CreateTxErrorInvalidAddress struct {
	message string
}

func NewCreateTxErrorInvalidAddress() *CreateTxError {
	return &CreateTxError{err: &CreateTxErrorInvalidAddress{}}
}

func (e CreateTxErrorInvalidAddress) destroy() {
}

func (err CreateTxErrorInvalidAddress) Error() string {
	return fmt.Sprintf("InvalidAddress: %s", err.message)
}

func (self CreateTxErrorInvalidAddress) Is(target error) bool {
	return target == ErrCreateTxErrorInvalidAddress
}

type CreateTxErrorCreateTx struct {
	message string
}

func NewCreateTxErrorCreateTx() *CreateTxError {
	return &CreateTxError{err: &CreateTxErrorCreateTx{}}
}

func (e CreateTxErrorCreateTx) destroy() {
}

func (err CreateTxErrorCreateTx) Error() string {
	return fmt.Sprintf("CreateTx: %s", err.message)
}

func (self CreateTxErrorCreateTx) Is(target error) bool {
	return target == ErrCreateTxErrorCreateTx
}

type CreateTxErrorSignTx struct {
	message string
}

func NewCreateTxErrorSignTx() *CreateTxError {
	return &CreateTxError{err: &CreateTxErrorSignTx{}}
}

func (e CreateTxErrorSignTx) destroy() {
}

func (err CreateTxErrorSignTx) Error() string {
	return fmt.Sprintf("SignTx: %s", err.message)
}

func (self CreateTxErrorSignTx) Is(target error) bool {
	return target == ErrCreateTxErrorSignTx
}

type FfiConverterCreateTxError struct{}

var FfiConverterCreateTxErrorINSTANCE = FfiConverterCreateTxError{}

func (c FfiConverterCreateTxError) Lift(eb RustBufferI) *CreateTxError {
	return LiftFromRustBuffer[*CreateTxError](c, eb)
}

func (c FfiConverterCreateTxError) Lower(value *CreateTxError) C.RustBuffer {
	return LowerIntoRustBuffer[*CreateTxError](c, value)
}

func (c FfiConverterCreateTxError) Read(reader io.Reader) *CreateTxError {
	errorID := readUint32(reader)

	message := FfiConverterStringINSTANCE.Read(reader)
	switch errorID {
	case 1:
		return &CreateTxError{&CreateTxErrorInvalidAddress{message}}
	case 2:
		return &CreateTxError{&CreateTxErrorCreateTx{message}}
	case 3:
		return &CreateTxError{&CreateTxErrorSignTx{message}}
	default:
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterCreateTxError.Read()", errorID))
	}

}

func (c FfiConverterCreateTxError) Write(writer io.Writer, value *CreateTxError) {
	switch variantValue := value.err.(type) {
	case *CreateTxErrorInvalidAddress:
		writeInt32(writer, 1)
	case *CreateTxErrorCreateTx:
		writeInt32(writer, 2)
	case *CreateTxErrorSignTx:
		writeInt32(writer, 3)
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiConverterCreateTxError.Write", value))
	}
}

type FfiDestroyerCreateTxError struct{}

func (_ FfiDestroyerCreateTxError) Destroy(value *CreateTxError) {
	switch variantValue := value.err.(type) {
	case CreateTxErrorInvalidAddress:
		variantValue.destroy()
	case CreateTxErrorCreateTx:
		variantValue.destroy()
	case CreateTxErrorSignTx:
		variantValue.destroy()
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiDestroyerCreateTxError.Destroy", value))
	}
}

type DatabaseError struct {
	err error
}

// Convience method to turn *DatabaseError into error
// Avoiding treating nil pointer as non nil error interface
func (err *DatabaseError) AsError() error {
	if err == nil {
		return nil
	} else {
		return err
	}
}

func (err DatabaseError) Error() string {
	return fmt.Sprintf("DatabaseError: %s", err.err.Error())
}

func (err DatabaseError) Unwrap() error {
	return err.err
}

// Err* are used for checking error type with `errors.Is`
var ErrDatabaseErrorWrite = fmt.Errorf("DatabaseErrorWrite")

// Variant structs
type DatabaseErrorWrite struct {
	message string
}

func NewDatabaseErrorWrite() *DatabaseError {
	return &DatabaseError{err: &DatabaseErrorWrite{}}
}

func (e DatabaseErrorWrite) destroy() {
}

func (err DatabaseErrorWrite) Error() string {
	return fmt.Sprintf("Write: %s", err.message)
}

func (self DatabaseErrorWrite) Is(target error) bool {
	return target == ErrDatabaseErrorWrite
}

type FfiConverterDatabaseError struct{}

var FfiConverterDatabaseErrorINSTANCE = FfiConverterDatabaseError{}

func (c FfiConverterDatabaseError) Lift(eb RustBufferI) *DatabaseError {
	return LiftFromRustBuffer[*DatabaseError](c, eb)
}

func (c FfiConverterDatabaseError) Lower(value *DatabaseError) C.RustBuffer {
	return LowerIntoRustBuffer[*DatabaseError](c, value)
}

func (c FfiConverterDatabaseError) Read(reader io.Reader) *DatabaseError {
	errorID := readUint32(reader)

	message := FfiConverterStringINSTANCE.Read(reader)
	switch errorID {
	case 1:
		return &DatabaseError{&DatabaseErrorWrite{message}}
	default:
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterDatabaseError.Read()", errorID))
	}

}

func (c FfiConverterDatabaseError) Write(writer io.Writer, value *DatabaseError) {
	switch variantValue := value.err.(type) {
	case *DatabaseErrorWrite:
		writeInt32(writer, 1)
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiConverterDatabaseError.Write", value))
	}
}

type FfiDestroyerDatabaseError struct{}

func (_ FfiDestroyerDatabaseError) Destroy(value *DatabaseError) {
	switch variantValue := value.err.(type) {
	case DatabaseErrorWrite:
		variantValue.destroy()
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiDestroyerDatabaseError.Destroy", value))
	}
}

type LoadError struct {
	err error
}

// Convience method to turn *LoadError into error
// Avoiding treating nil pointer as non nil error interface
func (err *LoadError) AsError() error {
	if err == nil {
		return nil
	} else {
		return err
	}
}

func (err LoadError) Error() string {
	return fmt.Sprintf("LoadError: %s", err.err.Error())
}

func (err LoadError) Unwrap() error {
	return err.err
}

// Err* are used for checking error type with `errors.Is`
var ErrLoadErrorDatabase = fmt.Errorf("LoadErrorDatabase")
var ErrLoadErrorReadHeader = fmt.Errorf("LoadErrorReadHeader")
var ErrLoadErrorParseHeader = fmt.Errorf("LoadErrorParseHeader")
var ErrLoadErrorHeaderVersion = fmt.Errorf("LoadErrorHeaderVersion")
var ErrLoadErrorWallet = fmt.Errorf("LoadErrorWallet")

// Variant structs
type LoadErrorDatabase struct {
	message string
}

func NewLoadErrorDatabase() *LoadError {
	return &LoadError{err: &LoadErrorDatabase{}}
}

func (e LoadErrorDatabase) destroy() {
}

func (err LoadErrorDatabase) Error() string {
	return fmt.Sprintf("Database: %s", err.message)
}

func (self LoadErrorDatabase) Is(target error) bool {
	return target == ErrLoadErrorDatabase
}

type LoadErrorReadHeader struct {
	message string
}

func NewLoadErrorReadHeader() *LoadError {
	return &LoadError{err: &LoadErrorReadHeader{}}
}

func (e LoadErrorReadHeader) destroy() {
}

func (err LoadErrorReadHeader) Error() string {
	return fmt.Sprintf("ReadHeader: %s", err.message)
}

func (self LoadErrorReadHeader) Is(target error) bool {
	return target == ErrLoadErrorReadHeader
}

type LoadErrorParseHeader struct {
	message string
}

func NewLoadErrorParseHeader() *LoadError {
	return &LoadError{err: &LoadErrorParseHeader{}}
}

func (e LoadErrorParseHeader) destroy() {
}

func (err LoadErrorParseHeader) Error() string {
	return fmt.Sprintf("ParseHeader: %s", err.message)
}

func (self LoadErrorParseHeader) Is(target error) bool {
	return target == ErrLoadErrorParseHeader
}

type LoadErrorHeaderVersion struct {
	message string
}

func NewLoadErrorHeaderVersion() *LoadError {
	return &LoadError{err: &LoadErrorHeaderVersion{}}
}

func (e LoadErrorHeaderVersion) destroy() {
}

func (err LoadErrorHeaderVersion) Error() string {
	return fmt.Sprintf("HeaderVersion: %s", err.message)
}

func (self LoadErrorHeaderVersion) Is(target error) bool {
	return target == ErrLoadErrorHeaderVersion
}

type LoadErrorWallet struct {
	message string
}

func NewLoadErrorWallet() *LoadError {
	return &LoadError{err: &LoadErrorWallet{}}
}

func (e LoadErrorWallet) destroy() {
}

func (err LoadErrorWallet) Error() string {
	return fmt.Sprintf("Wallet: %s", err.message)
}

func (self LoadErrorWallet) Is(target error) bool {
	return target == ErrLoadErrorWallet
}

type FfiConverterLoadError struct{}

var FfiConverterLoadErrorINSTANCE = FfiConverterLoadError{}

func (c FfiConverterLoadError) Lift(eb RustBufferI) *LoadError {
	return LiftFromRustBuffer[*LoadError](c, eb)
}

func (c FfiConverterLoadError) Lower(value *LoadError) C.RustBuffer {
	return LowerIntoRustBuffer[*LoadError](c, value)
}

func (c FfiConverterLoadError) Read(reader io.Reader) *LoadError {
	errorID := readUint32(reader)

	message := FfiConverterStringINSTANCE.Read(reader)
	switch errorID {
	case 1:
		return &LoadError{&LoadErrorDatabase{message}}
	case 2:
		return &LoadError{&LoadErrorReadHeader{message}}
	case 3:
		return &LoadError{&LoadErrorParseHeader{message}}
	case 4:
		return &LoadError{&LoadErrorHeaderVersion{message}}
	case 5:
		return &LoadError{&LoadErrorWallet{message}}
	default:
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterLoadError.Read()", errorID))
	}

}

func (c FfiConverterLoadError) Write(writer io.Writer, value *LoadError) {
	switch variantValue := value.err.(type) {
	case *LoadErrorDatabase:
		writeInt32(writer, 1)
	case *LoadErrorReadHeader:
		writeInt32(writer, 2)
	case *LoadErrorParseHeader:
		writeInt32(writer, 3)
	case *LoadErrorHeaderVersion:
		writeInt32(writer, 4)
	case *LoadErrorWallet:
		writeInt32(writer, 5)
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiConverterLoadError.Write", value))
	}
}

type FfiDestroyerLoadError struct{}

func (_ FfiDestroyerLoadError) Destroy(value *LoadError) {
	switch variantValue := value.err.(type) {
	case LoadErrorDatabase:
		variantValue.destroy()
	case LoadErrorReadHeader:
		variantValue.destroy()
	case LoadErrorParseHeader:
		variantValue.destroy()
	case LoadErrorHeaderVersion:
		variantValue.destroy()
	case LoadErrorWallet:
		variantValue.destroy()
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiDestroyerLoadError.Destroy", value))
	}
}

type FfiConverterSequenceString struct{}

var FfiConverterSequenceStringINSTANCE = FfiConverterSequenceString{}

func (c FfiConverterSequenceString) Lift(rb RustBufferI) []string {
	return LiftFromRustBuffer[[]string](c, rb)
}

func (c FfiConverterSequenceString) Read(reader io.Reader) []string {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]string, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterStringINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceString) Lower(value []string) C.RustBuffer {
	return LowerIntoRustBuffer[[]string](c, value)
}

func (c FfiConverterSequenceString) Write(writer io.Writer, value []string) {
	if len(value) > math.MaxInt32 {
		panic("[]string is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterStringINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceString struct{}

func (FfiDestroyerSequenceString) Destroy(sequence []string) {
	for _, value := range sequence {
		FfiDestroyerString{}.Destroy(value)
	}
}

type FfiConverterSequenceBytes struct{}

var FfiConverterSequenceBytesINSTANCE = FfiConverterSequenceBytes{}

func (c FfiConverterSequenceBytes) Lift(rb RustBufferI) [][]byte {
	return LiftFromRustBuffer[[][]byte](c, rb)
}

func (c FfiConverterSequenceBytes) Read(reader io.Reader) [][]byte {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([][]byte, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterBytesINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceBytes) Lower(value [][]byte) C.RustBuffer {
	return LowerIntoRustBuffer[[][]byte](c, value)
}

func (c FfiConverterSequenceBytes) Write(writer io.Writer, value [][]byte) {
	if len(value) > math.MaxInt32 {
		panic("[][]byte is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterBytesINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceBytes struct{}

func (FfiDestroyerSequenceBytes) Destroy(sequence [][]byte) {
	for _, value := range sequence {
		FfiDestroyerBytes{}.Destroy(value)
	}
}

type FfiConverterSequenceBlockId struct{}

var FfiConverterSequenceBlockIdINSTANCE = FfiConverterSequenceBlockId{}

func (c FfiConverterSequenceBlockId) Lift(rb RustBufferI) []BlockId {
	return LiftFromRustBuffer[[]BlockId](c, rb)
}

func (c FfiConverterSequenceBlockId) Read(reader io.Reader) []BlockId {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]BlockId, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterBlockIdINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceBlockId) Lower(value []BlockId) C.RustBuffer {
	return LowerIntoRustBuffer[[]BlockId](c, value)
}

func (c FfiConverterSequenceBlockId) Write(writer io.Writer, value []BlockId) {
	if len(value) > math.MaxInt32 {
		panic("[]BlockId is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterBlockIdINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceBlockId struct{}

func (FfiDestroyerSequenceBlockId) Destroy(sequence []BlockId) {
	for _, value := range sequence {
		FfiDestroyerBlockId{}.Destroy(value)
	}
}

type FfiConverterSequenceMempoolTx struct{}

var FfiConverterSequenceMempoolTxINSTANCE = FfiConverterSequenceMempoolTx{}

func (c FfiConverterSequenceMempoolTx) Lift(rb RustBufferI) []MempoolTx {
	return LiftFromRustBuffer[[]MempoolTx](c, rb)
}

func (c FfiConverterSequenceMempoolTx) Read(reader io.Reader) []MempoolTx {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]MempoolTx, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterMempoolTxINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceMempoolTx) Lower(value []MempoolTx) C.RustBuffer {
	return LowerIntoRustBuffer[[]MempoolTx](c, value)
}

func (c FfiConverterSequenceMempoolTx) Write(writer io.Writer, value []MempoolTx) {
	if len(value) > math.MaxInt32 {
		panic("[]MempoolTx is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterMempoolTxINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceMempoolTx struct{}

func (FfiDestroyerSequenceMempoolTx) Destroy(sequence []MempoolTx) {
	for _, value := range sequence {
		FfiDestroyerMempoolTx{}.Destroy(value)
	}
}

type FfiConverterSequenceRecipient struct{}

var FfiConverterSequenceRecipientINSTANCE = FfiConverterSequenceRecipient{}

func (c FfiConverterSequenceRecipient) Lift(rb RustBufferI) []Recipient {
	return LiftFromRustBuffer[[]Recipient](c, rb)
}

func (c FfiConverterSequenceRecipient) Read(reader io.Reader) []Recipient {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]Recipient, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterRecipientINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceRecipient) Lower(value []Recipient) C.RustBuffer {
	return LowerIntoRustBuffer[[]Recipient](c, value)
}

func (c FfiConverterSequenceRecipient) Write(writer io.Writer, value []Recipient) {
	if len(value) > math.MaxInt32 {
		panic("[]Recipient is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterRecipientINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceRecipient struct{}

func (FfiDestroyerSequenceRecipient) Destroy(sequence []Recipient) {
	for _, value := range sequence {
		FfiDestroyerRecipient{}.Destroy(value)
	}
}

type FfiConverterSequenceTxInfo struct{}

var FfiConverterSequenceTxInfoINSTANCE = FfiConverterSequenceTxInfo{}

func (c FfiConverterSequenceTxInfo) Lift(rb RustBufferI) []TxInfo {
	return LiftFromRustBuffer[[]TxInfo](c, rb)
}

func (c FfiConverterSequenceTxInfo) Read(reader io.Reader) []TxInfo {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]TxInfo, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterTxInfoINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceTxInfo) Lower(value []TxInfo) C.RustBuffer {
	return LowerIntoRustBuffer[[]TxInfo](c, value)
}

func (c FfiConverterSequenceTxInfo) Write(writer io.Writer, value []TxInfo) {
	if len(value) > math.MaxInt32 {
		panic("[]TxInfo is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterTxInfoINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceTxInfo struct{}

func (FfiDestroyerSequenceTxInfo) Destroy(sequence []TxInfo) {
	for _, value := range sequence {
		FfiDestroyerTxInfo{}.Destroy(value)
	}
}

type FfiConverterSequenceUtxoInfo struct{}

var FfiConverterSequenceUtxoInfoINSTANCE = FfiConverterSequenceUtxoInfo{}

func (c FfiConverterSequenceUtxoInfo) Lift(rb RustBufferI) []UtxoInfo {
	return LiftFromRustBuffer[[]UtxoInfo](c, rb)
}

func (c FfiConverterSequenceUtxoInfo) Read(reader io.Reader) []UtxoInfo {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]UtxoInfo, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterUtxoInfoINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceUtxoInfo) Lower(value []UtxoInfo) C.RustBuffer {
	return LowerIntoRustBuffer[[]UtxoInfo](c, value)
}

func (c FfiConverterSequenceUtxoInfo) Write(writer io.Writer, value []UtxoInfo) {
	if len(value) > math.MaxInt32 {
		panic("[]UtxoInfo is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterUtxoInfoINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceUtxoInfo struct{}

func (FfiDestroyerSequenceUtxoInfo) Destroy(sequence []UtxoInfo) {
	for _, value := range sequence {
		FfiDestroyerUtxoInfo{}.Destroy(value)
	}
}
