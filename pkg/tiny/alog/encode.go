package alog

import (
	"fmt"
	"strconv"
	"unsafe"
)

const (
	Separator = '\t'
	Delimiter = '\n'
)

func Trim(b []byte) []byte {
	i := len(b) - 1
	if i >= 0 && b[i] == Separator {
		b = b[:i]
	}
	return b
}
func LF(b []byte) []byte {
	return append(Trim(b), Delimiter)
}
func HT(b []byte) []byte {
	return append(b, Separator)
}

func AppendBool(b []byte, v bool) []byte {
	return HT(strconv.AppendBool(b, v))
}

func AppendInt(b []byte, v int) []byte {
	return AppendInt64(b, int64(v))
}
func AppendInt8(b []byte, v int8) []byte {
	return AppendInt64(b, int64(v))
}
func AppendInt16(b []byte, v int16) []byte {
	return AppendInt64(b, int64(v))
}
func AppendInt32(b []byte, v int32) []byte {
	return AppendInt64(b, int64(v))
}
func AppendInt64(b []byte, v int64) []byte {
	return HT(strconv.AppendInt(b, v, 10))
}

func AppendUint(b []byte, v uint) []byte {
	return AppendUint64(b, uint64(v))
}
func AppendUint8(b []byte, v uint8) []byte {
	return AppendUint64(b, uint64(v))
}
func AppendUint16(b []byte, v uint16) []byte {
	return AppendUint64(b, uint64(v))
}
func AppendUint32(b []byte, v uint32) []byte {
	return AppendUint64(b, uint64(v))
}
func AppendUint64(b []byte, v uint64) []byte {
	return HT(strconv.AppendUint(b, v, 10))
}

func AppendFloat32(b []byte, v float32) []byte {
	return HT(strconv.AppendFloat(b, float64(v), 'g', -1, 32))
}
func AppendFloat64(b []byte, v float64) []byte {
	return HT(strconv.AppendFloat(b, v, 'g', -1, 64))
}

func AppendByte(b []byte, v byte) []byte {
	return HT(append(b, Escape(v)...))
}

func AppendString(b []byte, v string) []byte {
	for i := 0; i < len(v); i++ {
		b = append(b, Escape(v[i])...)
	}
	return HT(b)
}

func AppendInterface(b []byte, i interface{}) []byte {
	switch v := i.(type) {
	case Byte:
		b = AppendByte(b, byte(v))
	case nil:
		b = HT(b)
	case bool:
		b = AppendBool(b, v)
	case int:
		b = AppendInt(b, v)
	case int8:
		b = AppendInt8(b, v)
	case int16:
		b = AppendInt16(b, v)
	case int32:
		b = AppendInt32(b, v)
	case int64:
		b = AppendInt64(b, v)
	case uint:
		b = AppendUint(b, v)
	case uint8:
		b = AppendUint8(b, v)
	case uint16:
		b = AppendUint16(b, v)
	case uint32:
		b = AppendUint32(b, v)
	case uint64:
		b = AppendUint64(b, v)
	case float32:
		b = AppendFloat32(b, v)
	case float64:
		b = AppendFloat64(b, v)
	case string:
		b = AppendString(b, v)
	case []byte:
		b = AppendString(b, NoCopy(v))
	default:
		b = AppendString(b, fmt.Sprint(i))
	}
	return b
}

func NoCopy(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

type Byte byte
