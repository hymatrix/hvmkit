package counter_test

import (
	"fmt"

	"github.com/hymatrix/hvmkit/examples/counter"
	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

func Example() {
	vm, err := counter.Spawn(vmmSchema.Env{})
	if err != nil {
		panic(err)
	}
	defer vm.Close()
	fmt.Println(vm.Apply("sender", vmmSchema.Meta{Action: "Submit"}).Output)
	snapshot, err := vm.Checkpoint()
	if err != nil {
		panic(err)
	}

	restored, err := counter.Spawn(vmmSchema.Env{})
	if err != nil {
		panic(err)
	}
	defer restored.Close()
	if err := restored.Restore(snapshot); err != nil {
		panic(err)
	}
	fmt.Println(restored.Apply("sender", vmmSchema.Meta{Action: "Submit"}).Output)
	// Output:
	// 1
	// 2
}
