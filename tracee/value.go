package tracee

import (
	"debug/dwarf"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type value interface {
	String() string
	Size() int64
}

type int8Value struct {
	*dwarf.IntType
	val int8
}

func (v int8Value) String() string {
	return fmt.Sprintf("%d", v.val)
}

type int16Value struct {
	*dwarf.IntType
	val int16
}

func (v int16Value) String() string {
	return fmt.Sprintf("%d", v.val)
}

type int32Value struct {
	*dwarf.IntType
	val int32
}

func (v int32Value) String() string {
	return fmt.Sprintf("%d", v.val)
}

type int64Value struct {
	*dwarf.IntType
	val int64
}

func (v int64Value) String() string {
	return fmt.Sprintf("%d", v.val)
}

type uint8Value struct {
	*dwarf.UintType
	val uint8
}

func (v uint8Value) String() string {
	return fmt.Sprintf("%d", v.val)
}

type uint16Value struct {
	*dwarf.UintType
	val uint16
}

func (v uint16Value) String() string {
	return fmt.Sprintf("%d", v.val)
}

type uint32Value struct {
	*dwarf.UintType
	val uint32
}

func (v uint32Value) String() string {
	return fmt.Sprintf("%d", v.val)
}

type uint64Value struct {
	*dwarf.UintType
	val uint64
}

func (v uint64Value) String() string {
	return fmt.Sprintf("%d", v.val)
}

type float32Value struct {
	*dwarf.FloatType
	val float32
}

func (v float32Value) String() string {
	return fmt.Sprintf("%g", v.val)
}

type float64Value struct {
	*dwarf.FloatType
	val float64
}

func (v float64Value) String() string {
	return fmt.Sprintf("%g", v.val)
}

type complex64Value struct {
	*dwarf.ComplexType
	val complex64
}

func (v complex64Value) String() string {
	return fmt.Sprintf("%g", v.val)
}

type complex128Value struct {
	*dwarf.ComplexType
	val complex128
}

func (v complex128Value) String() string {
	return fmt.Sprintf("%g", v.val)
}

type boolValue struct {
	*dwarf.BoolType
	val bool
}

func (v boolValue) String() string {
	return fmt.Sprintf("%t", v.val)
}

type ptrValue struct {
	*dwarf.PtrType
	addr       uint64
	pointedVal value
}

func (v ptrValue) String() string {
	return fmt.Sprintf("&%s", v.pointedVal)
}

type funcValue struct {
	*dwarf.FuncType
	addr uint64
}

func (v funcValue) String() string {
	return fmt.Sprintf("%#x", v.addr)
}

type stringValue struct {
	*dwarf.StructType
	val string
}

func (v stringValue) String() string {
	return strconv.Quote(v.val)
}

type sliceValue struct {
	*dwarf.StructType
	val []value
}

func (v sliceValue) String() string {
	var vals []string
	for _, v := range v.val {
		vals = append(vals, v.String())
	}
	return fmt.Sprintf("[]{%s}", strings.Join(vals, ", "))
}

type structValue struct {
	*dwarf.StructType
	fields map[string]value
}

func (v structValue) String() string {
	var vals []string
	for name, val := range v.fields {
		vals = append(vals, fmt.Sprintf("%s: %s", name, val))
	}
	return fmt.Sprintf("{%s}", strings.Join(vals, ", "))
}

type interfaceValue struct {
	*dwarf.StructType
	implType dwarf.Type
	implVal  value
}

func (v interfaceValue) String() string {
	return fmt.Sprintf("%s%s", v.implType, v.implVal)
}

type arrayValue struct {
	*dwarf.ArrayType
	val []value
}

func (v arrayValue) String() string {
	var vals []string
	for _, v := range v.val {
		vals = append(vals, v.String())
	}
	return fmt.Sprintf("[%d]{%s}", len(vals), strings.Join(vals, ", "))
}

type mapValue struct {
	*dwarf.TypedefType
	val map[value]value
}

func (v mapValue) String() string {
	var vals []string
	for k, v := range v.val {
		vals = append(vals, fmt.Sprintf("%s: %s", k, v))
	}
	return fmt.Sprintf("{%s}", strings.Join(vals, ", "))
}

type valueBuilder struct {
	reader memoryReader
	mapper runtimeTypeMapper
}

type memoryReader interface {
	ReadMemory(addr uint64, out []byte) error
}

type runtimeTypeMapper interface {
	// mapRuntimeType maps the specified runtime type address to the dwarf.Type.
	// It is necessary to find the type which implements the interface.
	mapRuntimeType(addr uint64) (dwarf.Type, error)
}

func (b valueBuilder) buildValue(rawTyp dwarf.Type, val []byte) value {
	switch typ := rawTyp.(type) {
	case *dwarf.IntType:
		switch typ.Size() {
		case 1:
			return int8Value{IntType: typ, val: int8(val[0])}
		case 2:
			return int16Value{IntType: typ, val: int16(binary.LittleEndian.Uint16(val))}
		case 4:
			return int32Value{IntType: typ, val: int32(binary.LittleEndian.Uint32(val))}
		case 8:
			return int64Value{IntType: typ, val: int64(binary.LittleEndian.Uint64(val))}
		}

	case *dwarf.UintType:
		switch typ.Size() {
		case 1:
			return uint8Value{UintType: typ, val: val[0]}
		case 2:
			return uint16Value{UintType: typ, val: binary.LittleEndian.Uint16(val)}
		case 4:
			return uint32Value{UintType: typ, val: binary.LittleEndian.Uint32(val)}
		case 8:
			return uint64Value{UintType: typ, val: binary.LittleEndian.Uint64(val)}
		}

	case *dwarf.FloatType:
		switch typ.Size() {
		case 4:
			return float32Value{FloatType: typ, val: math.Float32frombits(binary.LittleEndian.Uint32(val))}
		case 8:
			return float64Value{FloatType: typ, val: math.Float64frombits(binary.LittleEndian.Uint64(val))}
		}

	case *dwarf.ComplexType:
		switch typ.Size() {
		case 8:
			real := math.Float32frombits(binary.LittleEndian.Uint32(val[0:4]))
			img := math.Float32frombits(binary.LittleEndian.Uint32(val[4:8]))
			return complex64Value{ComplexType: typ, val: complex(real, img)}
		case 16:
			real := math.Float64frombits(binary.LittleEndian.Uint64(val[0:8]))
			img := math.Float64frombits(binary.LittleEndian.Uint64(val[8:16]))
			return complex128Value{ComplexType: typ, val: complex(real, img)}
		}

	case *dwarf.BoolType:
		return boolValue{BoolType: typ, val: val[0] == 1}

	case *dwarf.PtrType:
		addr := binary.LittleEndian.Uint64(val)
		if addr == 0 {
			return nil
		}
		buff := make([]byte, typ.Type.Size())
		if err := b.reader.ReadMemory(addr, buff); err != nil {
			break
		}
		pointedVal := b.buildValue(typ.Type, buff)
		return ptrValue{PtrType: typ, addr: addr, pointedVal: pointedVal}

	case *dwarf.FuncType:
		// TODO: print the pointer to the actual function (and the variables in closure if possible).
		addr := binary.LittleEndian.Uint64(val)
		return funcValue{FuncType: typ, addr: addr}

	case *dwarf.StructType:
		switch {
		case typ.StructName == "string":
			return b.buildStringValue(typ, val)
		case strings.HasPrefix(typ.StructName, "[]"):
			return b.buildSliceValue(typ, val)
		case typ.StructName == "runtime.iface":
			return b.buildInterfaceValue(typ, val)
		default:
			return b.buildStructValue(typ, val)
		}
	case *dwarf.ArrayType:
		if typ.Count == -1 {
			return nil
		}
		var vals []value
		stride := int(typ.Type.Size())
		for i := 0; i < int(typ.Count); i++ {
			vals = append(vals, b.buildValue(typ.Type, val[i*stride:(i+1)*stride]))
		}
		return arrayValue{ArrayType: typ, val: vals}
	case *dwarf.TypedefType:
		if strings.HasPrefix(typ.String(), "map[") {
			return b.buildMapValue(typ, val)
		}
		return b.buildValue(typ.Type, val)
	}
	return nil
}

func (b valueBuilder) buildStringValue(typ *dwarf.StructType, val []byte) stringValue {
	addr := binary.LittleEndian.Uint64(val[:8])
	len := int(binary.LittleEndian.Uint64(val[8:]))
	buff := make([]byte, len)
	if err := b.reader.ReadMemory(addr, buff); err != nil {
		return stringValue{}
	}
	return stringValue{StructType: typ, val: string(buff)}
}

func (b valueBuilder) buildSliceValue(typ *dwarf.StructType, val []byte) sliceValue {
	structVal := b.buildStructValue(typ, val)
	len := int(structVal.fields["len"].(int64Value).val)
	firstElem := structVal.fields["array"].(ptrValue)
	sliceVal := sliceValue{StructType: typ, val: []value{firstElem.pointedVal}}

	for i := 1; i < len; i++ {
		addr := firstElem.addr + uint64(firstElem.pointedVal.Size())*uint64(i)
		buff := make([]byte, 8)
		binary.LittleEndian.PutUint64(buff, addr)
		elem := b.buildValue(firstElem.PtrType, buff).(ptrValue)
		sliceVal.val = append(sliceVal.val, elem.pointedVal)
	}

	return sliceVal
}

func (b valueBuilder) buildInterfaceValue(typ *dwarf.StructType, val []byte) interfaceValue {
	// TODO: need to limit the nest level or parse only a part of struct.
	structVal := b.buildStructValue(typ, val)
	data := structVal.fields["data"].(ptrValue)

	if b.mapper == nil {
		// Old go versions offer the different method to map the runtime type.
		return interfaceValue{StructType: typ}
	}

	tab := structVal.fields["tab"].(ptrValue).pointedVal.(structValue)
	runtimeTypeAddr := tab.fields["_type"].(ptrValue).addr
	implType, err := b.mapper.mapRuntimeType(runtimeTypeAddr)
	if err != nil {
		return interfaceValue{}
	}

	dataBuff := make([]byte, implType.Size())
	if err := b.reader.ReadMemory(data.addr, dataBuff); err != nil {
		return interfaceValue{}
	}

	return interfaceValue{StructType: typ, implType: implType, implVal: b.buildValue(implType, dataBuff)}
}

func (b valueBuilder) buildStructValue(typ *dwarf.StructType, val []byte) structValue {
	fields := make(map[string]value)
	for _, field := range typ.Field {
		fields[field.Name] = b.buildValue(field.Type, val[field.ByteOffset:field.ByteOffset+field.Type.Size()])
	}
	return structValue{StructType: typ, fields: fields}
}

func (b valueBuilder) buildMapValue(typ *dwarf.TypedefType, val []byte) mapValue {
	ptrVal := b.buildValue(typ.Type, val)
	hmapVal := ptrVal.(ptrValue).pointedVal.(structValue)
	numBuckets := 1 << hmapVal.fields["B"].(uint8Value).val
	ptrToBuckets := hmapVal.fields["buckets"].(ptrValue)

	// TODO: handle overflow case
	kv := make(map[value]value)
	for i := 0; ; i++ {
		buckets := ptrToBuckets.pointedVal.(structValue)
		tophash := buckets.fields["tophash"].(arrayValue)
		keys := buckets.fields["keys"].(arrayValue)
		values := buckets.fields["values"].(arrayValue)

		for j, hash := range tophash.val {
			if hash.(uint8Value).val == 0 {
				continue
			}
			kv[keys.val[j]] = values.val[j]
		}

		if i+1 == numBuckets {
			break
		}

		addr := ptrToBuckets.addr + uint64(i+1)*uint64(buckets.Size())
		buff := make([]byte, 8)
		binary.LittleEndian.PutUint64(buff, addr)
		ptrToBuckets = b.buildValue(ptrToBuckets.PtrType, buff).(ptrValue)
	}

	return mapValue{TypedefType: typ, val: kv}
}
