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

type RustBuffer = C.RustBuffer

type RustBufferI interface {
	AsReader() *bytes.Reader
	Free()
	ToGoBytes() []byte
	Data() unsafe.Pointer
	Len() int
	Capacity() int
}

func RustBufferFromExternal(b RustBufferI) RustBuffer {
	return RustBuffer{
		capacity: C.int(b.Capacity()),
		len:      C.int(b.Len()),
		data:     (*C.uchar)(b.Data()),
	}
}

func (cb RustBuffer) Capacity() int {
	return int(cb.capacity)
}

func (cb RustBuffer) Len() int {
	return int(cb.len)
}

func (cb RustBuffer) Data() unsafe.Pointer {
	return unsafe.Pointer(cb.data)
}

func (cb RustBuffer) AsReader() *bytes.Reader {
	b := unsafe.Slice((*byte)(cb.data), C.int(cb.len))
	return bytes.NewReader(b)
}

func (cb RustBuffer) Free() {
	rustCall(func(status *C.RustCallStatus) bool {
		C.ffi_bdkgo_rustbuffer_free(cb, status)
		return false
	})
}

func (cb RustBuffer) ToGoBytes() []byte {
	return C.GoBytes(unsafe.Pointer(cb.data), C.int(cb.len))
}

func stringToRustBuffer(str string) RustBuffer {
	return bytesToRustBuffer([]byte(str))
}

func bytesToRustBuffer(b []byte) RustBuffer {
	if len(b) == 0 {
		return RustBuffer{}
	}
	// We can pass the pointer along here, as it is pinned
	// for the duration of this call
	foreign := C.ForeignBytes{
		len:  C.int(len(b)),
		data: (*C.uchar)(unsafe.Pointer(&b[0])),
	}

	return rustCall(func(status *C.RustCallStatus) RustBuffer {
		return C.ffi_bdkgo_rustbuffer_from_bytes(foreign, status)
	})
}

type BufLifter[GoType any] interface {
	Lift(value RustBufferI) GoType
}

type BufLowerer[GoType any] interface {
	Lower(value GoType) RustBuffer
}

type FfiConverter[GoType any, FfiType any] interface {
	Lift(value FfiType) GoType
	Lower(value GoType) FfiType
}

type BufReader[GoType any] interface {
	Read(reader io.Reader) GoType
}

type BufWriter[GoType any] interface {
	Write(writer io.Writer, value GoType)
}

type FfiRustBufConverter[GoType any, FfiType any] interface {
	FfiConverter[GoType, FfiType]
	BufReader[GoType]
}

func LowerIntoRustBuffer[GoType any](bufWriter BufWriter[GoType], value GoType) RustBuffer {
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

func rustCallWithError[U any](converter BufLifter[error], callback func(*C.RustCallStatus) U) (U, error) {
	var status C.RustCallStatus
	returnValue := callback(&status)
	err := checkCallStatus(converter, status)

	return returnValue, err
}

func checkCallStatus(converter BufLifter[error], status C.RustCallStatus) error {
	switch status.code {
	case 0:
		return nil
	case 1:
		return converter.Lift(status.errorBuf)
	case 2:
		// when the rust code sees a panic, it tries to construct a rustbuffer
		// with the message.  but if that code panics, then it just sends back
		// an empty buffer.
		if status.errorBuf.len > 0 {
			panic(fmt.Errorf("%s", FfiConverterStringINSTANCE.Lift(status.errorBuf)))
		} else {
			panic(fmt.Errorf("Rust panicked while handling Rust panic"))
		}
	default:
		return fmt.Errorf("unknown status code: %d", status.code)
	}
}

func checkCallStatusUnknown(status C.RustCallStatus) error {
	switch status.code {
	case 0:
		return nil
	case 1:
		panic(fmt.Errorf("function not returning an error returned an error"))
	case 2:
		// when the rust code sees a panic, it tries to construct a rustbuffer
		// with the message.  but if that code panics, then it just sends back
		// an empty buffer.
		if status.errorBuf.len > 0 {
			panic(fmt.Errorf("%s", FfiConverterStringINSTANCE.Lift(status.errorBuf)))
		} else {
			panic(fmt.Errorf("Rust panicked while handling Rust panic"))
		}
	default:
		return fmt.Errorf("unknown status code: %d", status.code)
	}
}

func rustCall[U any](callback func(*C.RustCallStatus) U) U {
	returnValue, err := rustCallWithError(nil, callback)
	if err != nil {
		panic(err)
	}
	return returnValue
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
	bindingsContractVersion := 24
	// Get the scaffolding contract version by calling the into the dylib
	scaffoldingContractVersion := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint32_t {
		return C.ffi_bdkgo_uniffi_contract_version(uniffiStatus)
	})
	if bindingsContractVersion != int(scaffoldingContractVersion) {
		// If this happens try cleaning and rebuilding your project
		panic("bdkgo: UniFFI contract version mismatch")
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_apply_block(uniffiStatus)
		})
		if checksum != 20455 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_apply_block: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_apply_mempool(uniffiStatus)
		})
		if checksum != 23416 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_apply_mempool: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_balance(uniffiStatus)
		})
		if checksum != 26195 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_balance: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_create_tx(uniffiStatus)
		})
		if checksum != 54855 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_create_tx: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_fresh_address(uniffiStatus)
		})
		if checksum != 39819 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_fresh_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_genesis_hash(uniffiStatus)
		})
		if checksum != 65013 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_genesis_hash: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_increment_reference_counter(uniffiStatus)
		})
		if checksum != 61284 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_increment_reference_counter: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_last_unused_address(uniffiStatus)
		})
		if checksum != 34780 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_last_unused_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_mnemonic_words(uniffiStatus)
		})
		if checksum != 3138 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_mnemonic_words: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_peek_address(uniffiStatus)
		})
		if checksum != 60510 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_peek_address: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_recent_blocks(uniffiStatus)
		})
		if checksum != 57902 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_recent_blocks: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_transactions(uniffiStatus)
		})
		if checksum != 39598 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_transactions: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_method_wallet_utxos(uniffiStatus)
		})
		if checksum != 53540 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_method_wallet_utxos: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_constructor_wallet_create_new(uniffiStatus)
		})
		if checksum != 23490 {
			// If this happens try cleaning and rebuilding your project
			panic("bdkgo: uniffi_bdkgo_checksum_constructor_wallet_create_new: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_bdkgo_checksum_constructor_wallet_load(uniffiStatus)
		})
		if checksum != 64083 {
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

type FfiConverterFloat32 struct{}

var FfiConverterFloat32INSTANCE = FfiConverterFloat32{}

func (FfiConverterFloat32) Lower(value float32) C.float {
	return C.float(value)
}

func (FfiConverterFloat32) Write(writer io.Writer, value float32) {
	writeFloat32(writer, value)
}

func (FfiConverterFloat32) Lift(value C.float) float32 {
	return float32(value)
}

func (FfiConverterFloat32) Read(reader io.Reader) float32 {
	return readFloat32(reader)
}

type FfiDestroyerFloat32 struct{}

func (FfiDestroyerFloat32) Destroy(_ float32) {}

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
	if err != nil {
		panic(err)
	}
	if read_length != int(length) {
		panic(fmt.Errorf("bad read length when reading string, expected %d, read %d", length, read_length))
	}
	return string(buffer)
}

func (FfiConverterString) Lower(value string) RustBuffer {
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

func (c FfiConverterBytes) Lower(value []byte) RustBuffer {
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
	if err != nil {
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
	pointer      unsafe.Pointer
	callCounter  atomic.Int64
	freeFunction func(unsafe.Pointer, *C.RustCallStatus)
	destroyed    atomic.Bool
}

func newFfiObject(pointer unsafe.Pointer, freeFunction func(unsafe.Pointer, *C.RustCallStatus)) FfiObject {
	return FfiObject{
		pointer:      pointer,
		freeFunction: freeFunction,
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

	return ffiObject.pointer
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

type Wallet struct {
	ffiObject FfiObject
}

func WalletCreateNew(dbPath string, network string, genesisHash []byte) (*Wallet, error) {
	_uniffiRV, _uniffiErr := rustCallWithError(FfiConverterTypeCreateNewError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_bdkgo_fn_constructor_wallet_create_new(FfiConverterStringINSTANCE.Lower(dbPath), FfiConverterStringINSTANCE.Lower(network), FfiConverterBytesINSTANCE.Lower(genesisHash), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *Wallet
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterWalletINSTANCE.Lift(_uniffiRV), _uniffiErr
	}
}
func WalletLoad(dbPath string) (*Wallet, error) {
	_uniffiRV, _uniffiErr := rustCallWithError(FfiConverterTypeLoadError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_bdkgo_fn_constructor_wallet_load(FfiConverterStringINSTANCE.Lower(dbPath), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *Wallet
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterWalletINSTANCE.Lift(_uniffiRV), _uniffiErr
	}
}

func (_self *Wallet) ApplyBlock(height uint32, blockBytes []byte) error {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError(FfiConverterTypeApplyBlockError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_bdkgo_fn_method_wallet_apply_block(
			_pointer, FfiConverterUint32INSTANCE.Lower(height), FfiConverterBytesINSTANCE.Lower(blockBytes), _uniffiStatus)
		return false
	})
	return _uniffiErr
}

func (_self *Wallet) ApplyMempool(txs []MempoolTx) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	rustCall(func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_bdkgo_fn_method_wallet_apply_mempool(
			_pointer, FfiConverterSequenceTypeMempoolTxINSTANCE.Lower(txs), _uniffiStatus)
		return false
	})
}

func (_self *Wallet) Balance() Balance {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterTypeBalanceINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return C.uniffi_bdkgo_fn_method_wallet_balance(
			_pointer, _uniffiStatus)
	}))
}

func (_self *Wallet) CreateTx(feerate float32, recipients []Recipient) ([]byte, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError(FfiConverterTypeCreateTxError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return C.uniffi_bdkgo_fn_method_wallet_create_tx(
			_pointer, FfiConverterFloat32INSTANCE.Lower(feerate), FfiConverterSequenceTypeRecipientINSTANCE.Lower(recipients), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue []byte
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterBytesINSTANCE.Lift(_uniffiRV), _uniffiErr
	}
}

func (_self *Wallet) FreshAddress() (AddressInfo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError(FfiConverterTypeDatabaseError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return C.uniffi_bdkgo_fn_method_wallet_fresh_address(
			_pointer, _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue AddressInfo
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterTypeAddressInfoINSTANCE.Lift(_uniffiRV), _uniffiErr
	}
}

func (_self *Wallet) GenesisHash() []byte {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return C.uniffi_bdkgo_fn_method_wallet_genesis_hash(
			_pointer, _uniffiStatus)
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
	_uniffiRV, _uniffiErr := rustCallWithError(FfiConverterTypeDatabaseError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return C.uniffi_bdkgo_fn_method_wallet_last_unused_address(
			_pointer, _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue AddressInfo
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterTypeAddressInfoINSTANCE.Lift(_uniffiRV), _uniffiErr
	}
}

func (_self *Wallet) MnemonicWords() []string {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterSequenceStringINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return C.uniffi_bdkgo_fn_method_wallet_mnemonic_words(
			_pointer, _uniffiStatus)
	}))
}

func (_self *Wallet) PeekAddress(index uint32) (AddressInfo, error) {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError(FfiConverterTypeDatabaseError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return C.uniffi_bdkgo_fn_method_wallet_peek_address(
			_pointer, FfiConverterUint32INSTANCE.Lower(index), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue AddressInfo
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterTypeAddressInfoINSTANCE.Lift(_uniffiRV), _uniffiErr
	}
}

func (_self *Wallet) RecentBlocks(count uint32) []BlockId {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterSequenceTypeBlockIdINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return C.uniffi_bdkgo_fn_method_wallet_recent_blocks(
			_pointer, FfiConverterUint32INSTANCE.Lower(count), _uniffiStatus)
	}))
}

func (_self *Wallet) Transactions() []TxInfo {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterSequenceTypeTxInfoINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return C.uniffi_bdkgo_fn_method_wallet_transactions(
			_pointer, _uniffiStatus)
	}))
}

func (_self *Wallet) Utxos() []UtxoInfo {
	_pointer := _self.ffiObject.incrementPointer("*Wallet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterSequenceTypeUtxoInfoINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return C.uniffi_bdkgo_fn_method_wallet_utxos(
			_pointer, _uniffiStatus)
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
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_bdkgo_fn_free_wallet(pointer, status)
			}),
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

type FfiConverterTypeAddressInfo struct{}

var FfiConverterTypeAddressInfoINSTANCE = FfiConverterTypeAddressInfo{}

func (c FfiConverterTypeAddressInfo) Lift(rb RustBufferI) AddressInfo {
	return LiftFromRustBuffer[AddressInfo](c, rb)
}

func (c FfiConverterTypeAddressInfo) Read(reader io.Reader) AddressInfo {
	return AddressInfo{
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
	}
}

func (c FfiConverterTypeAddressInfo) Lower(value AddressInfo) RustBuffer {
	return LowerIntoRustBuffer[AddressInfo](c, value)
}

func (c FfiConverterTypeAddressInfo) Write(writer io.Writer, value AddressInfo) {
	FfiConverterUint32INSTANCE.Write(writer, value.Index)
	FfiConverterStringINSTANCE.Write(writer, value.Address)
}

type FfiDestroyerTypeAddressInfo struct{}

func (_ FfiDestroyerTypeAddressInfo) Destroy(value AddressInfo) {
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

type FfiConverterTypeBalance struct{}

var FfiConverterTypeBalanceINSTANCE = FfiConverterTypeBalance{}

func (c FfiConverterTypeBalance) Lift(rb RustBufferI) Balance {
	return LiftFromRustBuffer[Balance](c, rb)
}

func (c FfiConverterTypeBalance) Read(reader io.Reader) Balance {
	return Balance{
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterTypeBalance) Lower(value Balance) RustBuffer {
	return LowerIntoRustBuffer[Balance](c, value)
}

func (c FfiConverterTypeBalance) Write(writer io.Writer, value Balance) {
	FfiConverterUint64INSTANCE.Write(writer, value.Immature)
	FfiConverterUint64INSTANCE.Write(writer, value.TrustedPending)
	FfiConverterUint64INSTANCE.Write(writer, value.UntrustedPending)
	FfiConverterUint64INSTANCE.Write(writer, value.Confirmed)
}

type FfiDestroyerTypeBalance struct{}

func (_ FfiDestroyerTypeBalance) Destroy(value Balance) {
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

type FfiConverterTypeBlockId struct{}

var FfiConverterTypeBlockIdINSTANCE = FfiConverterTypeBlockId{}

func (c FfiConverterTypeBlockId) Lift(rb RustBufferI) BlockId {
	return LiftFromRustBuffer[BlockId](c, rb)
}

func (c FfiConverterTypeBlockId) Read(reader io.Reader) BlockId {
	return BlockId{
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterBytesINSTANCE.Read(reader),
	}
}

func (c FfiConverterTypeBlockId) Lower(value BlockId) RustBuffer {
	return LowerIntoRustBuffer[BlockId](c, value)
}

func (c FfiConverterTypeBlockId) Write(writer io.Writer, value BlockId) {
	FfiConverterUint32INSTANCE.Write(writer, value.Height)
	FfiConverterBytesINSTANCE.Write(writer, value.Hash)
}

type FfiDestroyerTypeBlockId struct{}

func (_ FfiDestroyerTypeBlockId) Destroy(value BlockId) {
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

type FfiConverterTypeMempoolTx struct{}

var FfiConverterTypeMempoolTxINSTANCE = FfiConverterTypeMempoolTx{}

func (c FfiConverterTypeMempoolTx) Lift(rb RustBufferI) MempoolTx {
	return LiftFromRustBuffer[MempoolTx](c, rb)
}

func (c FfiConverterTypeMempoolTx) Read(reader io.Reader) MempoolTx {
	return MempoolTx{
		FfiConverterBytesINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterTypeMempoolTx) Lower(value MempoolTx) RustBuffer {
	return LowerIntoRustBuffer[MempoolTx](c, value)
}

func (c FfiConverterTypeMempoolTx) Write(writer io.Writer, value MempoolTx) {
	FfiConverterBytesINSTANCE.Write(writer, value.Tx)
	FfiConverterUint64INSTANCE.Write(writer, value.AddedUnix)
}

type FfiDestroyerTypeMempoolTx struct{}

func (_ FfiDestroyerTypeMempoolTx) Destroy(value MempoolTx) {
	value.Destroy()
}

type Recipient struct {
	ScriptPubkey []byte
	Amount       uint64
}

func (r *Recipient) Destroy() {
	FfiDestroyerBytes{}.Destroy(r.ScriptPubkey)
	FfiDestroyerUint64{}.Destroy(r.Amount)
}

type FfiConverterTypeRecipient struct{}

var FfiConverterTypeRecipientINSTANCE = FfiConverterTypeRecipient{}

func (c FfiConverterTypeRecipient) Lift(rb RustBufferI) Recipient {
	return LiftFromRustBuffer[Recipient](c, rb)
}

func (c FfiConverterTypeRecipient) Read(reader io.Reader) Recipient {
	return Recipient{
		FfiConverterBytesINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
	}
}

func (c FfiConverterTypeRecipient) Lower(value Recipient) RustBuffer {
	return LowerIntoRustBuffer[Recipient](c, value)
}

func (c FfiConverterTypeRecipient) Write(writer io.Writer, value Recipient) {
	FfiConverterBytesINSTANCE.Write(writer, value.ScriptPubkey)
	FfiConverterUint64INSTANCE.Write(writer, value.Amount)
}

type FfiDestroyerTypeRecipient struct{}

func (_ FfiDestroyerTypeRecipient) Destroy(value Recipient) {
	value.Destroy()
}

type TxInfo struct {
	Txid          []byte
	Tx            []byte
	Spent         uint64
	Received      uint64
	Confirmations *uint32
}

func (r *TxInfo) Destroy() {
	FfiDestroyerBytes{}.Destroy(r.Txid)
	FfiDestroyerBytes{}.Destroy(r.Tx)
	FfiDestroyerUint64{}.Destroy(r.Spent)
	FfiDestroyerUint64{}.Destroy(r.Received)
	FfiDestroyerOptionalUint32{}.Destroy(r.Confirmations)
}

type FfiConverterTypeTxInfo struct{}

var FfiConverterTypeTxInfoINSTANCE = FfiConverterTypeTxInfo{}

func (c FfiConverterTypeTxInfo) Lift(rb RustBufferI) TxInfo {
	return LiftFromRustBuffer[TxInfo](c, rb)
}

func (c FfiConverterTypeTxInfo) Read(reader io.Reader) TxInfo {
	return TxInfo{
		FfiConverterBytesINSTANCE.Read(reader),
		FfiConverterBytesINSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterOptionalUint32INSTANCE.Read(reader),
	}
}

func (c FfiConverterTypeTxInfo) Lower(value TxInfo) RustBuffer {
	return LowerIntoRustBuffer[TxInfo](c, value)
}

func (c FfiConverterTypeTxInfo) Write(writer io.Writer, value TxInfo) {
	FfiConverterBytesINSTANCE.Write(writer, value.Txid)
	FfiConverterBytesINSTANCE.Write(writer, value.Tx)
	FfiConverterUint64INSTANCE.Write(writer, value.Spent)
	FfiConverterUint64INSTANCE.Write(writer, value.Received)
	FfiConverterOptionalUint32INSTANCE.Write(writer, value.Confirmations)
}

type FfiDestroyerTypeTxInfo struct{}

func (_ FfiDestroyerTypeTxInfo) Destroy(value TxInfo) {
	value.Destroy()
}

type UtxoInfo struct {
	Txid            []byte
	Vout            uint32
	Amount          uint64
	ScriptPubkey    []byte
	IsChange        bool
	DerivationIndex uint32
	Confirmations   *uint32
}

func (r *UtxoInfo) Destroy() {
	FfiDestroyerBytes{}.Destroy(r.Txid)
	FfiDestroyerUint32{}.Destroy(r.Vout)
	FfiDestroyerUint64{}.Destroy(r.Amount)
	FfiDestroyerBytes{}.Destroy(r.ScriptPubkey)
	FfiDestroyerBool{}.Destroy(r.IsChange)
	FfiDestroyerUint32{}.Destroy(r.DerivationIndex)
	FfiDestroyerOptionalUint32{}.Destroy(r.Confirmations)
}

type FfiConverterTypeUtxoInfo struct{}

var FfiConverterTypeUtxoInfoINSTANCE = FfiConverterTypeUtxoInfo{}

func (c FfiConverterTypeUtxoInfo) Lift(rb RustBufferI) UtxoInfo {
	return LiftFromRustBuffer[UtxoInfo](c, rb)
}

func (c FfiConverterTypeUtxoInfo) Read(reader io.Reader) UtxoInfo {
	return UtxoInfo{
		FfiConverterBytesINSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterUint64INSTANCE.Read(reader),
		FfiConverterBytesINSTANCE.Read(reader),
		FfiConverterBoolINSTANCE.Read(reader),
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterOptionalUint32INSTANCE.Read(reader),
	}
}

func (c FfiConverterTypeUtxoInfo) Lower(value UtxoInfo) RustBuffer {
	return LowerIntoRustBuffer[UtxoInfo](c, value)
}

func (c FfiConverterTypeUtxoInfo) Write(writer io.Writer, value UtxoInfo) {
	FfiConverterBytesINSTANCE.Write(writer, value.Txid)
	FfiConverterUint32INSTANCE.Write(writer, value.Vout)
	FfiConverterUint64INSTANCE.Write(writer, value.Amount)
	FfiConverterBytesINSTANCE.Write(writer, value.ScriptPubkey)
	FfiConverterBoolINSTANCE.Write(writer, value.IsChange)
	FfiConverterUint32INSTANCE.Write(writer, value.DerivationIndex)
	FfiConverterOptionalUint32INSTANCE.Write(writer, value.Confirmations)
}

type FfiDestroyerTypeUtxoInfo struct{}

func (_ FfiDestroyerTypeUtxoInfo) Destroy(value UtxoInfo) {
	value.Destroy()
}

type ApplyBlockError struct {
	err error
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
	return &ApplyBlockError{
		err: &ApplyBlockErrorDecodeBlock{},
	}
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
	return &ApplyBlockError{
		err: &ApplyBlockErrorCannotConnect{},
	}
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
	return &ApplyBlockError{
		err: &ApplyBlockErrorDatabase{},
	}
}

func (err ApplyBlockErrorDatabase) Error() string {
	return fmt.Sprintf("Database: %s", err.message)
}

func (self ApplyBlockErrorDatabase) Is(target error) bool {
	return target == ErrApplyBlockErrorDatabase
}

type FfiConverterTypeApplyBlockError struct{}

var FfiConverterTypeApplyBlockErrorINSTANCE = FfiConverterTypeApplyBlockError{}

func (c FfiConverterTypeApplyBlockError) Lift(eb RustBufferI) error {
	return LiftFromRustBuffer[error](c, eb)
}

func (c FfiConverterTypeApplyBlockError) Lower(value *ApplyBlockError) RustBuffer {
	return LowerIntoRustBuffer[*ApplyBlockError](c, value)
}

func (c FfiConverterTypeApplyBlockError) Read(reader io.Reader) error {
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
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterTypeApplyBlockError.Read()", errorID))
	}

}

func (c FfiConverterTypeApplyBlockError) Write(writer io.Writer, value *ApplyBlockError) {
	switch variantValue := value.err.(type) {
	case *ApplyBlockErrorDecodeBlock:
		writeInt32(writer, 1)
	case *ApplyBlockErrorCannotConnect:
		writeInt32(writer, 2)
	case *ApplyBlockErrorDatabase:
		writeInt32(writer, 3)
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiConverterTypeApplyBlockError.Write", value))
	}
}

type CreateNewError struct {
	err error
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
	return &CreateNewError{
		err: &CreateNewErrorParseNetwork{},
	}
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
	return &CreateNewError{
		err: &CreateNewErrorParseGenesisHash{},
	}
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
	return &CreateNewError{
		err: &CreateNewErrorDatabase{},
	}
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
	return &CreateNewError{
		err: &CreateNewErrorWallet{},
	}
}

func (err CreateNewErrorWallet) Error() string {
	return fmt.Sprintf("Wallet: %s", err.message)
}

func (self CreateNewErrorWallet) Is(target error) bool {
	return target == ErrCreateNewErrorWallet
}

type FfiConverterTypeCreateNewError struct{}

var FfiConverterTypeCreateNewErrorINSTANCE = FfiConverterTypeCreateNewError{}

func (c FfiConverterTypeCreateNewError) Lift(eb RustBufferI) error {
	return LiftFromRustBuffer[error](c, eb)
}

func (c FfiConverterTypeCreateNewError) Lower(value *CreateNewError) RustBuffer {
	return LowerIntoRustBuffer[*CreateNewError](c, value)
}

func (c FfiConverterTypeCreateNewError) Read(reader io.Reader) error {
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
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterTypeCreateNewError.Read()", errorID))
	}

}

func (c FfiConverterTypeCreateNewError) Write(writer io.Writer, value *CreateNewError) {
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
		panic(fmt.Sprintf("invalid error value `%v` in FfiConverterTypeCreateNewError.Write", value))
	}
}

type CreateTxError struct {
	err error
}

func (err CreateTxError) Error() string {
	return fmt.Sprintf("CreateTxError: %s", err.err.Error())
}

func (err CreateTxError) Unwrap() error {
	return err.err
}

// Err* are used for checking error type with `errors.Is`
var ErrCreateTxErrorCreateTx = fmt.Errorf("CreateTxErrorCreateTx")
var ErrCreateTxErrorSignTx = fmt.Errorf("CreateTxErrorSignTx")

// Variant structs
type CreateTxErrorCreateTx struct {
	message string
}

func NewCreateTxErrorCreateTx() *CreateTxError {
	return &CreateTxError{
		err: &CreateTxErrorCreateTx{},
	}
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
	return &CreateTxError{
		err: &CreateTxErrorSignTx{},
	}
}

func (err CreateTxErrorSignTx) Error() string {
	return fmt.Sprintf("SignTx: %s", err.message)
}

func (self CreateTxErrorSignTx) Is(target error) bool {
	return target == ErrCreateTxErrorSignTx
}

type FfiConverterTypeCreateTxError struct{}

var FfiConverterTypeCreateTxErrorINSTANCE = FfiConverterTypeCreateTxError{}

func (c FfiConverterTypeCreateTxError) Lift(eb RustBufferI) error {
	return LiftFromRustBuffer[error](c, eb)
}

func (c FfiConverterTypeCreateTxError) Lower(value *CreateTxError) RustBuffer {
	return LowerIntoRustBuffer[*CreateTxError](c, value)
}

func (c FfiConverterTypeCreateTxError) Read(reader io.Reader) error {
	errorID := readUint32(reader)

	message := FfiConverterStringINSTANCE.Read(reader)
	switch errorID {
	case 1:
		return &CreateTxError{&CreateTxErrorCreateTx{message}}
	case 2:
		return &CreateTxError{&CreateTxErrorSignTx{message}}
	default:
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterTypeCreateTxError.Read()", errorID))
	}

}

func (c FfiConverterTypeCreateTxError) Write(writer io.Writer, value *CreateTxError) {
	switch variantValue := value.err.(type) {
	case *CreateTxErrorCreateTx:
		writeInt32(writer, 1)
	case *CreateTxErrorSignTx:
		writeInt32(writer, 2)
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiConverterTypeCreateTxError.Write", value))
	}
}

type DatabaseError struct {
	err error
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
	return &DatabaseError{
		err: &DatabaseErrorWrite{},
	}
}

func (err DatabaseErrorWrite) Error() string {
	return fmt.Sprintf("Write: %s", err.message)
}

func (self DatabaseErrorWrite) Is(target error) bool {
	return target == ErrDatabaseErrorWrite
}

type FfiConverterTypeDatabaseError struct{}

var FfiConverterTypeDatabaseErrorINSTANCE = FfiConverterTypeDatabaseError{}

func (c FfiConverterTypeDatabaseError) Lift(eb RustBufferI) error {
	return LiftFromRustBuffer[error](c, eb)
}

func (c FfiConverterTypeDatabaseError) Lower(value *DatabaseError) RustBuffer {
	return LowerIntoRustBuffer[*DatabaseError](c, value)
}

func (c FfiConverterTypeDatabaseError) Read(reader io.Reader) error {
	errorID := readUint32(reader)

	message := FfiConverterStringINSTANCE.Read(reader)
	switch errorID {
	case 1:
		return &DatabaseError{&DatabaseErrorWrite{message}}
	default:
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterTypeDatabaseError.Read()", errorID))
	}

}

func (c FfiConverterTypeDatabaseError) Write(writer io.Writer, value *DatabaseError) {
	switch variantValue := value.err.(type) {
	case *DatabaseErrorWrite:
		writeInt32(writer, 1)
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiConverterTypeDatabaseError.Write", value))
	}
}

type LoadError struct {
	err error
}

func (err LoadError) Error() string {
	return fmt.Sprintf("LoadError: %s", err.err.Error())
}

func (err LoadError) Unwrap() error {
	return err.err
}

// Err* are used for checking error type with `errors.Is`
var ErrLoadErrorDatabase = fmt.Errorf("LoadErrorDatabase")
var ErrLoadErrorParseHeader = fmt.Errorf("LoadErrorParseHeader")
var ErrLoadErrorHeaderVersion = fmt.Errorf("LoadErrorHeaderVersion")
var ErrLoadErrorWallet = fmt.Errorf("LoadErrorWallet")

// Variant structs
type LoadErrorDatabase struct {
	message string
}

func NewLoadErrorDatabase() *LoadError {
	return &LoadError{
		err: &LoadErrorDatabase{},
	}
}

func (err LoadErrorDatabase) Error() string {
	return fmt.Sprintf("Database: %s", err.message)
}

func (self LoadErrorDatabase) Is(target error) bool {
	return target == ErrLoadErrorDatabase
}

type LoadErrorParseHeader struct {
	message string
}

func NewLoadErrorParseHeader() *LoadError {
	return &LoadError{
		err: &LoadErrorParseHeader{},
	}
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
	return &LoadError{
		err: &LoadErrorHeaderVersion{},
	}
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
	return &LoadError{
		err: &LoadErrorWallet{},
	}
}

func (err LoadErrorWallet) Error() string {
	return fmt.Sprintf("Wallet: %s", err.message)
}

func (self LoadErrorWallet) Is(target error) bool {
	return target == ErrLoadErrorWallet
}

type FfiConverterTypeLoadError struct{}

var FfiConverterTypeLoadErrorINSTANCE = FfiConverterTypeLoadError{}

func (c FfiConverterTypeLoadError) Lift(eb RustBufferI) error {
	return LiftFromRustBuffer[error](c, eb)
}

func (c FfiConverterTypeLoadError) Lower(value *LoadError) RustBuffer {
	return LowerIntoRustBuffer[*LoadError](c, value)
}

func (c FfiConverterTypeLoadError) Read(reader io.Reader) error {
	errorID := readUint32(reader)

	message := FfiConverterStringINSTANCE.Read(reader)
	switch errorID {
	case 1:
		return &LoadError{&LoadErrorDatabase{message}}
	case 2:
		return &LoadError{&LoadErrorParseHeader{message}}
	case 3:
		return &LoadError{&LoadErrorHeaderVersion{message}}
	case 4:
		return &LoadError{&LoadErrorWallet{message}}
	default:
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterTypeLoadError.Read()", errorID))
	}

}

func (c FfiConverterTypeLoadError) Write(writer io.Writer, value *LoadError) {
	switch variantValue := value.err.(type) {
	case *LoadErrorDatabase:
		writeInt32(writer, 1)
	case *LoadErrorParseHeader:
		writeInt32(writer, 2)
	case *LoadErrorHeaderVersion:
		writeInt32(writer, 3)
	case *LoadErrorWallet:
		writeInt32(writer, 4)
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiConverterTypeLoadError.Write", value))
	}
}

type FfiConverterOptionalUint32 struct{}

var FfiConverterOptionalUint32INSTANCE = FfiConverterOptionalUint32{}

func (c FfiConverterOptionalUint32) Lift(rb RustBufferI) *uint32 {
	return LiftFromRustBuffer[*uint32](c, rb)
}

func (_ FfiConverterOptionalUint32) Read(reader io.Reader) *uint32 {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterUint32INSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalUint32) Lower(value *uint32) RustBuffer {
	return LowerIntoRustBuffer[*uint32](c, value)
}

func (_ FfiConverterOptionalUint32) Write(writer io.Writer, value *uint32) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterUint32INSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalUint32 struct{}

func (_ FfiDestroyerOptionalUint32) Destroy(value *uint32) {
	if value != nil {
		FfiDestroyerUint32{}.Destroy(*value)
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

func (c FfiConverterSequenceString) Lower(value []string) RustBuffer {
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

type FfiConverterSequenceTypeBlockId struct{}

var FfiConverterSequenceTypeBlockIdINSTANCE = FfiConverterSequenceTypeBlockId{}

func (c FfiConverterSequenceTypeBlockId) Lift(rb RustBufferI) []BlockId {
	return LiftFromRustBuffer[[]BlockId](c, rb)
}

func (c FfiConverterSequenceTypeBlockId) Read(reader io.Reader) []BlockId {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]BlockId, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterTypeBlockIdINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceTypeBlockId) Lower(value []BlockId) RustBuffer {
	return LowerIntoRustBuffer[[]BlockId](c, value)
}

func (c FfiConverterSequenceTypeBlockId) Write(writer io.Writer, value []BlockId) {
	if len(value) > math.MaxInt32 {
		panic("[]BlockId is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterTypeBlockIdINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceTypeBlockId struct{}

func (FfiDestroyerSequenceTypeBlockId) Destroy(sequence []BlockId) {
	for _, value := range sequence {
		FfiDestroyerTypeBlockId{}.Destroy(value)
	}
}

type FfiConverterSequenceTypeMempoolTx struct{}

var FfiConverterSequenceTypeMempoolTxINSTANCE = FfiConverterSequenceTypeMempoolTx{}

func (c FfiConverterSequenceTypeMempoolTx) Lift(rb RustBufferI) []MempoolTx {
	return LiftFromRustBuffer[[]MempoolTx](c, rb)
}

func (c FfiConverterSequenceTypeMempoolTx) Read(reader io.Reader) []MempoolTx {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]MempoolTx, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterTypeMempoolTxINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceTypeMempoolTx) Lower(value []MempoolTx) RustBuffer {
	return LowerIntoRustBuffer[[]MempoolTx](c, value)
}

func (c FfiConverterSequenceTypeMempoolTx) Write(writer io.Writer, value []MempoolTx) {
	if len(value) > math.MaxInt32 {
		panic("[]MempoolTx is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterTypeMempoolTxINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceTypeMempoolTx struct{}

func (FfiDestroyerSequenceTypeMempoolTx) Destroy(sequence []MempoolTx) {
	for _, value := range sequence {
		FfiDestroyerTypeMempoolTx{}.Destroy(value)
	}
}

type FfiConverterSequenceTypeRecipient struct{}

var FfiConverterSequenceTypeRecipientINSTANCE = FfiConverterSequenceTypeRecipient{}

func (c FfiConverterSequenceTypeRecipient) Lift(rb RustBufferI) []Recipient {
	return LiftFromRustBuffer[[]Recipient](c, rb)
}

func (c FfiConverterSequenceTypeRecipient) Read(reader io.Reader) []Recipient {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]Recipient, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterTypeRecipientINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceTypeRecipient) Lower(value []Recipient) RustBuffer {
	return LowerIntoRustBuffer[[]Recipient](c, value)
}

func (c FfiConverterSequenceTypeRecipient) Write(writer io.Writer, value []Recipient) {
	if len(value) > math.MaxInt32 {
		panic("[]Recipient is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterTypeRecipientINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceTypeRecipient struct{}

func (FfiDestroyerSequenceTypeRecipient) Destroy(sequence []Recipient) {
	for _, value := range sequence {
		FfiDestroyerTypeRecipient{}.Destroy(value)
	}
}

type FfiConverterSequenceTypeTxInfo struct{}

var FfiConverterSequenceTypeTxInfoINSTANCE = FfiConverterSequenceTypeTxInfo{}

func (c FfiConverterSequenceTypeTxInfo) Lift(rb RustBufferI) []TxInfo {
	return LiftFromRustBuffer[[]TxInfo](c, rb)
}

func (c FfiConverterSequenceTypeTxInfo) Read(reader io.Reader) []TxInfo {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]TxInfo, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterTypeTxInfoINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceTypeTxInfo) Lower(value []TxInfo) RustBuffer {
	return LowerIntoRustBuffer[[]TxInfo](c, value)
}

func (c FfiConverterSequenceTypeTxInfo) Write(writer io.Writer, value []TxInfo) {
	if len(value) > math.MaxInt32 {
		panic("[]TxInfo is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterTypeTxInfoINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceTypeTxInfo struct{}

func (FfiDestroyerSequenceTypeTxInfo) Destroy(sequence []TxInfo) {
	for _, value := range sequence {
		FfiDestroyerTypeTxInfo{}.Destroy(value)
	}
}

type FfiConverterSequenceTypeUtxoInfo struct{}

var FfiConverterSequenceTypeUtxoInfoINSTANCE = FfiConverterSequenceTypeUtxoInfo{}

func (c FfiConverterSequenceTypeUtxoInfo) Lift(rb RustBufferI) []UtxoInfo {
	return LiftFromRustBuffer[[]UtxoInfo](c, rb)
}

func (c FfiConverterSequenceTypeUtxoInfo) Read(reader io.Reader) []UtxoInfo {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]UtxoInfo, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterTypeUtxoInfoINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceTypeUtxoInfo) Lower(value []UtxoInfo) RustBuffer {
	return LowerIntoRustBuffer[[]UtxoInfo](c, value)
}

func (c FfiConverterSequenceTypeUtxoInfo) Write(writer io.Writer, value []UtxoInfo) {
	if len(value) > math.MaxInt32 {
		panic("[]UtxoInfo is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterTypeUtxoInfoINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceTypeUtxoInfo struct{}

func (FfiDestroyerSequenceTypeUtxoInfo) Destroy(sequence []UtxoInfo) {
	for _, value := range sequence {
		FfiDestroyerTypeUtxoInfo{}.Destroy(value)
	}
}
