//go:build darwin

package tray

import (
	"fmt"
	"os"
	"structs"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type nsSize struct {
	_             structs.HostLayout
	Width, Height float64
}

type nsPoint struct {
	_    structs.HostLayout
	X, Y float64
}

const (
	activationPolicyAccessory  = 1
	variableStatusItemLength   = -1.0
	controlStateOff            = 0
	controlStateOn             = 1
	eventTypeApplicationDefine = 15
)

var (
	selAlloc                = objc.RegisterName("alloc")
	selNew                  = objc.RegisterName("new")
	selRetain               = objc.RegisterName("retain")
	selDrain                = objc.RegisterName("drain")
	selSharedApplication    = objc.RegisterName("sharedApplication")
	selSetActivationPolicy  = objc.RegisterName("setActivationPolicy:")
	selRun                  = objc.RegisterName("run")
	selStop                 = objc.RegisterName("stop:")
	selPostEvent            = objc.RegisterName("postEvent:atStart:")
	selSystemStatusBar      = objc.RegisterName("systemStatusBar")
	selStatusItemWithLength = objc.RegisterName("statusItemWithLength:")
	selButton               = objc.RegisterName("button")
	selSetImage             = objc.RegisterName("setImage:")
	selSetTitle             = objc.RegisterName("setTitle:")
	selSetToolTip           = objc.RegisterName("setToolTip:")
	selSetMenu              = objc.RegisterName("setMenu:")
	selStringWithUTF8String = objc.RegisterName("stringWithUTF8String:")
	selDataWithBytesLength  = objc.RegisterName("dataWithBytes:length:")
	selInitWithData         = objc.RegisterName("initWithData:")
	selSetSize              = objc.RegisterName("setSize:")
	selSetTemplate          = objc.RegisterName("setTemplate:")
	selInitWithTitle        = objc.RegisterName("initWithTitle:")
	selInitItem             = objc.RegisterName("initWithTitle:action:keyEquivalent:")
	selSetTarget            = objc.RegisterName("setTarget:")
	selSetState             = objc.RegisterName("setState:")
	selAddItem              = objc.RegisterName("addItem:")
	selSeparatorItem        = objc.RegisterName("separatorItem")
	selSetDelegate          = objc.RegisterName("setDelegate:")
	selPerformOnMain        = objc.RegisterName("performSelectorOnMainThread:withObject:waitUntilDone:")
	selOtherEvent           = objc.RegisterName("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:")
	selActionOpen           = objc.RegisterName("linkctlOpen:")
	selActionToggleLogin    = objc.RegisterName("linkctlToggleLogin:")
	selActionQuit           = objc.RegisterName("linkctlQuit:")
	selActionStop           = objc.RegisterName("linkctlStop:")
	selMenuNeedsUpdate      = objc.RegisterName("menuNeedsUpdate:")
)

var (
	current   Menu
	app       objc.ID
	target    objc.ID
	loginItem objc.ID
	running   atomic.Bool
	stopEarly atomic.Bool

	setupOnce sync.Once
	setupErr  error
)

func setupRuntime() error {
	setupOnce.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_LAZY); err != nil {
			setupErr = fmt.Errorf("tray: dlopen AppKit: %w", err)
			return
		}
		cls, err := objc.RegisterClass("LinkctlTrayTarget", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: selActionOpen, Fn: onOpen},
			{Cmd: selActionToggleLogin, Fn: onToggleLogin},
			{Cmd: selActionQuit, Fn: onQuit},
			{Cmd: selActionStop, Fn: onStop},
			{Cmd: selMenuNeedsUpdate, Fn: onMenuNeedsUpdate},
		})
		if err != nil {
			setupErr = fmt.Errorf("tray: register class: %w", err)
			return
		}
		target = objc.ID(cls).Send(selNew)
	})
	return setupErr
}

func nsString(s string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(selStringWithUTF8String, s)
}

func nsImage(png []byte, template bool) objc.ID {
	if len(png) == 0 {
		return 0
	}
	data := objc.ID(objc.GetClass("NSData")).Send(selDataWithBytesLength, unsafe.Pointer(&png[0]), uint64(len(png)))
	img := objc.ID(objc.GetClass("NSImage")).Send(selAlloc).Send(selInitWithData, data)
	if img == 0 {
		return 0
	}
	img.Send(selSetSize, nsSize{Width: 18, Height: 18})
	img.Send(selSetTemplate, template)
	return img
}

func menuItem(title string, action objc.SEL) objc.ID {
	it := objc.ID(objc.GetClass("NSMenuItem")).Send(selAlloc).Send(selInitItem, nsString(title), action, nsString(""))
	it.Send(selSetTarget, target)
	return it
}

func loginState() int {
	if current.StartOnLogin != nil && current.StartOnLogin() {
		return controlStateOn
	}
	return controlStateOff
}

func run(m Menu, onReady func()) {
	if err := setupRuntime(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	current = m
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selNew)

	app = objc.ID(objc.GetClass("NSApplication")).Send(selSharedApplication)
	app.Send(selSetActivationPolicy, activationPolicyAccessory)

	bar := objc.ID(objc.GetClass("NSStatusBar")).Send(selSystemStatusBar)
	item := bar.Send(selStatusItemWithLength, float64(variableStatusItemLength))
	item.Send(selRetain)

	button := item.Send(selButton)
	img := nsImage(m.TemplatePNG, true)
	if img == 0 {
		img = nsImage(m.IconPNG, false)
	}
	if img != 0 {
		button.Send(selSetImage, img)
	} else {
		button.Send(selSetTitle, nsString("Link"))
	}
	if m.Tooltip != "" {
		button.Send(selSetToolTip, nsString(m.Tooltip))
	}

	menu := objc.ID(objc.GetClass("NSMenu")).Send(selAlloc).Send(selInitWithTitle, nsString("linkctl"))
	menu.Send(selAddItem, menuItem("Open control panel", selActionOpen))
	loginItem = menuItem("Start on login", selActionToggleLogin)
	loginItem.Send(selSetState, loginState())
	menu.Send(selAddItem, loginItem)
	menu.Send(selAddItem, objc.ID(objc.GetClass("NSMenuItem")).Send(selSeparatorItem))
	menu.Send(selAddItem, menuItem("Quit linkctl", selActionQuit))
	menu.Send(selSetDelegate, target)
	item.Send(selSetMenu, menu)

	pool.Send(selDrain)

	running.Store(true)
	if onReady != nil {
		go onReady()
	}
	if stopEarly.Load() {
		running.Store(false)
		return
	}
	app.Send(selRun)
	running.Store(false)
}

func quit() {
	if !running.Load() {
		stopEarly.Store(true)
		return
	}
	target.Send(selPerformOnMain, selActionStop, objc.ID(0), false)
}

// Action handlers run on the main thread; anything that may block is moved
// to a goroutine.

func onOpen(_ objc.ID, _ objc.SEL, _ objc.ID) {
	if current.OnOpen != nil {
		go current.OnOpen()
	}
}

func onToggleLogin(_ objc.ID, _ objc.SEL, _ objc.ID) {
	if current.StartOnLogin == nil || current.SetStartOnLogin == nil {
		return
	}
	enable := !current.StartOnLogin()
	if err := current.SetStartOnLogin(enable); err != nil {
		fmt.Fprintln(os.Stderr, "tray: start on login:", err)
	}
	loginItem.Send(selSetState, loginState())
}

func onQuit(_ objc.ID, _ objc.SEL, _ objc.ID) {
	go func() {
		if current.OnQuit != nil {
			current.OnQuit()
		}
		quit()
	}()
}

func onMenuNeedsUpdate(_ objc.ID, _ objc.SEL, _ objc.ID) {
	loginItem.Send(selSetState, loginState())
}

// onStop stops the run loop; a dummy event makes -run return immediately.
func onStop(_ objc.ID, _ objc.SEL, _ objc.ID) {
	app.Send(selStop, objc.ID(0))
	ev := objc.ID(objc.GetClass("NSEvent")).Send(selOtherEvent,
		uint64(eventTypeApplicationDefine), nsPoint{}, uint64(0), float64(0), int64(0), objc.ID(0), int16(0), int64(0), int64(0))
	app.Send(selPostEvent, ev, true)
}
