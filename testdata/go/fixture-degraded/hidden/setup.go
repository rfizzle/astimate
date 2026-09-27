package hidden

import "unsafe"

func init() {
	events <- limit * int(unsafe.Sizeof(counter))
}
