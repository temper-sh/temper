package machine

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Keep only the function binding for the process lifetime, not a device or its
// budget. The shipped binary remains cgo-free and needs no developer tools.
var loadMetal = sync.OnceValues(func() (func() objc.ID, error) {
	library, err := purego.Dlopen("/System/Library/Frameworks/Metal.framework/Metal", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("load system Metal framework: %w", err)
	}
	symbol, err := purego.Dlsym(library, "MTLCreateSystemDefaultDevice")
	if err != nil {
		_ = purego.Dlclose(library)
		return nil, fmt.Errorf("find default Metal device API: %w", err)
	}
	var createDevice func() objc.ID
	purego.RegisterFunc(&createDevice, symbol)
	return createDevice, nil
})

// metalWorkingSetBytes reads Apple's current recommendedMaxWorkingSetSize.
// This is a performance budget, not a hardware maximum or permission to set
// iogpu.wired_limit_mb to an arbitrary value.
func metalWorkingSetBytes(ctx context.Context) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	createDevice, err := loadMetal()
	if err != nil {
		return 0, err
	}
	// Objective-C autorelease pools are thread-local. Drain before allowing
	// this goroutine to migrate, including on errors or cancellation.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	poolClass := objc.GetClass("NSAutoreleasePool")
	if poolClass == 0 {
		return 0, errors.New("system autorelease pool class is unavailable")
	}
	pool := objc.ID(poolClass).Send(objc.RegisterName("alloc")).Send(objc.RegisterName("init"))
	if pool == 0 {
		return 0, errors.New("create Metal query autorelease pool")
	}
	defer pool.Send(objc.RegisterName("drain"))
	device := createDevice()
	if device == 0 {
		return 0, errors.New("no default Metal device is available")
	}
	// MTLCreateSystemDefaultDevice is NS_RETURNS_RETAINED.
	defer device.Send(objc.RegisterName("release"))
	value := objc.Send[uint64](device, objc.RegisterName("recommendedMaxWorkingSetSize"))
	return value, ctx.Err()
}
