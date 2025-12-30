package main

import (
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// Hook
	WH_KEYBOARD_LL = 13
	HC_ACTION      = 0
	LLKHF_INJECTED = 0x00000010

	WM_KEYDOWN    = 0x0100
	WM_KEYUP      = 0x0101
	WM_SYSKEYDOWN = 0x0104
	WM_SYSKEYUP   = 0x0105

	VK_CAPITAL = 0x14
	VK_SHIFT   = 0x10

	INPUT_KEYBOARD  = 1
	KEYEVENTF_KEYUP = 0x0002

	VK_CONTROL = 0x11
	VK_MENU    = 0x12 // Alt
	VK_LWIN    = 0x5B
	VK_SPACE   = 0x20

	// Tray/window
	WM_APP      = 0x8000
	WM_TRAYICON = WM_APP + 1

	WM_COMMAND = 0x0111
	WM_DESTROY = 0x0002
	WM_CLOSE   = 0x0010

	// NotifyIcon
	NIM_ADD    = 0x00000000
	NIM_MODIFY = 0x00000001
	NIM_DELETE = 0x00000002

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004

	// Mouse messages
	WM_RBUTTONUP = 0x0205
	WM_LBUTTONUP = 0x0202

	// Menus
	TPM_RIGHTBUTTON = 0x0002
	TPM_RETURNCMD   = 0x0100

	MF_STRING    = 0x0000
	MF_CHECKED   = 0x0008
	MF_UNCHECKED = 0x0000
	MF_SEPARATOR = 0x0800
	MF_POPUP     = 0x0010

	IDI_APPLICATION = 32512
	IDC_ARROW       = 32512

	CW_USEDEFAULT = 0x80000000
)

// Menu command IDs
const (
	CMD_TOGGLE_ENABLED = 1001
	CMD_MODE_CTRLSHIFT = 1101
	CMD_MODE_ALTSHIFT  = 1102
	CMD_MODE_WINSPACE  = 1103
	CMD_EXIT           = 1201
)

type SwitchMode int

const (
	ModeCtrlShift SwitchMode = iota
	ModeAltShift
	ModeWinSpace
)

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type KEYBDINPUT struct {
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

// On 64-bit Windows, INPUT must be 40 bytes.
// The union is 32 bytes; KEYBDINPUT is 24, so pad to 32.
type INPUT struct {
	Type uint32
	_    uint32 // alignment
	Ki   KEYBDINPUT
	_    [8]byte // pad union to 32 bytes (24 + 8)
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

type MSG struct {
	Hwnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type POINT struct {
	X int32
	Y int32
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             windows.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            windows.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UTimeoutVersion  uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     windows.Handle
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetAsyncKeyState    = user32.NewProc("GetAsyncKeyState")
	procSendInput           = user32.NewProc("SendInput")

	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")

	procLoadIconW   = user32.NewProc("LoadIconW")
	procLoadCursorW = user32.NewProc("LoadCursorW")

	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procSetForegroundWnd = user32.NewProc("SetForegroundWindow")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procCheckMenuItem    = user32.NewProc("CheckMenuItem")

	procShell_NotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

var (
	hHook      windows.Handle
	hwnd       windows.Handle
	enabled    = true
	switchMode = ModeAltShift

	menuRoot windows.Handle
	menuMode windows.Handle
)

func wstr(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

func isShiftDown() bool {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(VK_SHIFT))
	return (r & 0x8000) != 0
}

func sendKeyCombo(down []uint16, up []uint16) {
	inputs := make([]INPUT, 0, len(down)+len(up))

	for _, vk := range down {
		inputs = append(inputs, INPUT{
			Type: INPUT_KEYBOARD,
			Ki: KEYBDINPUT{
				WVk:     vk,
				DwFlags: 0,
			},
		})
	}
	for _, vk := range up {
		inputs = append(inputs, INPUT{
			Type: INPUT_KEYBOARD,
			Ki: KEYBDINPUT{
				WVk:     vk,
				DwFlags: KEYEVENTF_KEYUP,
			},
		})
	}

	if len(inputs) == 0 {
		return
	}
	_, _, _ = procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
}

func triggerLanguageSwitch() {
	switch switchMode {
	case ModeCtrlShift:
		sendKeyCombo([]uint16{VK_CONTROL, VK_SHIFT}, []uint16{VK_SHIFT, VK_CONTROL})
	case ModeAltShift:
		sendKeyCombo([]uint16{VK_MENU, VK_SHIFT}, []uint16{VK_SHIFT, VK_MENU})
	case ModeWinSpace:
		sendKeyCombo([]uint16{VK_LWIN, VK_SPACE}, []uint16{VK_SPACE, VK_LWIN})
	default:
		sendKeyCombo([]uint16{VK_CONTROL, VK_SHIFT}, []uint16{VK_SHIFT, VK_CONTROL})
	}
}

func callNext(nCode int, wParam uintptr, lParam uintptr) uintptr {
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

var hookProc = syscall.NewCallback(func(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode == HC_ACTION {
		if !enabled {
			return callNext(nCode, wParam, lParam)
		}

		kb := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))
		if (kb.Flags & LLKHF_INJECTED) != 0 {
			return callNext(nCode, wParam, lParam)
		}

		if kb.VkCode == VK_CAPITAL {
			// Shift+Caps => normal Caps Lock
			if isShiftDown() {
				return callNext(nCode, wParam, lParam)
			}

			// Caps alone => language switch, suppress Caps toggle
			if wParam == WM_KEYDOWN || wParam == WM_SYSKEYDOWN {
				triggerLanguageSwitch()
			}
			if wParam == WM_KEYDOWN || wParam == WM_SYSKEYDOWN || wParam == WM_KEYUP || wParam == WM_SYSKEYUP {
				return 1
			}
		}
	}
	return callNext(nCode, wParam, lParam)
})

func addTrayIcon() error {
	// Use default application icon
	hIcon, _, _ := procLoadIconW.Call(0, uintptr(IDI_APPLICATION))

	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = 1
	nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	nid.UCallbackMessage = WM_TRAYICON
	nid.HIcon = windows.Handle(hIcon)
	copy(nid.SzTip[:], windows.StringToUTF16("Caps → Language switch\nShift+Caps → CapsLock"))

	r, _, err := procShell_NotifyIconW.Call(uintptr(NIM_ADD), uintptr(unsafe.Pointer(&nid)))
	if r == 0 {
		return err
	}
	return nil
}

func removeTrayIcon() {
	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = 1
	_, _, _ = procShell_NotifyIconW.Call(uintptr(NIM_DELETE), uintptr(unsafe.Pointer(&nid)))
}

func buildMenus() {
	// Root popup
	h, _, _ := procCreatePopupMenu.Call()
	menuRoot = windows.Handle(h)

	// Mode submenu
	h2, _, _ := procCreatePopupMenu.Call()
	menuMode = windows.Handle(h2)

	_ = appendMenu(menuMode, MF_STRING, CMD_MODE_CTRLSHIFT, "Ctrl + Shift")
	_ = appendMenu(menuMode, MF_STRING, CMD_MODE_ALTSHIFT, "Alt + Shift")
	_ = appendMenu(menuMode, MF_STRING, CMD_MODE_WINSPACE, "Win + Space")

	_ = appendMenu(menuRoot, MF_STRING, CMD_TOGGLE_ENABLED, "Enabled")
	_ = appendMenu(menuRoot, MF_SEPARATOR, 0, "")
	_ = appendMenuPopup(menuRoot, menuMode, "Mode")
	_ = appendMenu(menuRoot, MF_SEPARATOR, 0, "")
	_ = appendMenu(menuRoot, MF_STRING, CMD_EXIT, "Exit")

	updateMenuChecks()
}

func appendMenu(hMenu windows.Handle, flags uint32, id uint32, text string) error {
	var txt *uint16
	if text != "" {
		txt = wstr(text)
	}
	r, _, err := procAppendMenuW.Call(uintptr(hMenu), uintptr(flags), uintptr(id), uintptr(unsafe.Pointer(txt)))
	if r == 0 {
		return err
	}
	return nil
}

func appendMenuPopup(hMenu windows.Handle, hSub windows.Handle, text string) error {
	r, _, err := procAppendMenuW.Call(
		uintptr(hMenu),
		uintptr(MF_POPUP|MF_STRING),
		uintptr(hSub),
		uintptr(unsafe.Pointer(wstr(text))),
	)
	if r == 0 {
		return err
	}
	return nil
}

func checkMenuItem(hMenu windows.Handle, id uint32, checked bool) {
	state := uint32(MF_UNCHECKED)
	if checked {
		state = MF_CHECKED
	}
	_, _, _ = procCheckMenuItem.Call(uintptr(hMenu), uintptr(id), uintptr(state))
}

func updateMenuChecks() {
	checkMenuItem(menuRoot, CMD_TOGGLE_ENABLED, enabled)

	checkMenuItem(menuMode, CMD_MODE_CTRLSHIFT, switchMode == ModeCtrlShift)
	checkMenuItem(menuMode, CMD_MODE_ALTSHIFT, switchMode == ModeAltShift)
	checkMenuItem(menuMode, CMD_MODE_WINSPACE, switchMode == ModeWinSpace)
}

func showTrayMenu() {
	var pt POINT
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	// Required for TrackPopupMenu to work reliably with tray icons
	_, _, _ = procSetForegroundWnd.Call(uintptr(hwnd))

	cmd, _, _ := procTrackPopupMenu.Call(
		uintptr(menuRoot),
		uintptr(TPM_RIGHTBUTTON|TPM_RETURNCMD),
		uintptr(pt.X),
		uintptr(pt.Y),
		0,
		uintptr(hwnd),
		0,
	)
	if cmd != 0 {
		handleCommand(uint32(cmd))
	}
}

func handleCommand(cmd uint32) {
	switch cmd {
	case CMD_TOGGLE_ENABLED:
		enabled = !enabled
		updateMenuChecks()

	case CMD_MODE_CTRLSHIFT:
		switchMode = ModeCtrlShift
		updateMenuChecks()
	case CMD_MODE_ALTSHIFT:
		switchMode = ModeAltShift
		updateMenuChecks()
	case CMD_MODE_WINSPACE:
		switchMode = ModeWinSpace
		updateMenuChecks()

	case CMD_EXIT:
		_, _, _ = procDestroyWindow.Call(uintptr(hwnd))
	}
}

var wndProc = syscall.NewCallback(func(hWnd uintptr, msg uint32, wParam uintptr, lParam uintptr) uintptr {
	switch msg {
	case WM_TRAYICON:
		// Left click: toggle enabled. Right click: menu.
		switch uint32(lParam) {
		case WM_LBUTTONUP:
			enabled = !enabled
			updateMenuChecks()
			return 0
		case WM_RBUTTONUP:
			showTrayMenu()
			return 0
		}
		return 0

	case WM_COMMAND:
		handleCommand(uint32(wParam & 0xffff))
		return 0

	case WM_CLOSE:
		_, _, _ = procDestroyWindow.Call(hWnd)
		return 0

	case WM_DESTROY:
		removeTrayIcon()
		if hHook != 0 {
			_, _, _ = procUnhookWindowsHookEx.Call(uintptr(hHook))
		}
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hWnd, uintptr(msg), wParam, lParam)
	return ret
})

func createHiddenWindow() error {
	hMod, _, _ := procGetModuleHandleW.Call(0)
	className := wstr("CapsLangSwitchTrayClass")

	// Register window class
	hCursor, _, _ := procLoadCursorW.Call(0, uintptr(IDC_ARROW))
	hIcon, _, _ := procLoadIconW.Call(0, uintptr(IDI_APPLICATION))

	var wc WNDCLASSEXW
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = wndProc
	wc.HInstance = windows.Handle(hMod)
	wc.HCursor = windows.Handle(hCursor)
	wc.HIcon = windows.Handle(hIcon)
	wc.HIconSm = windows.Handle(hIcon)
	wc.LpszClassName = className

	r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		// If already registered, Windows returns 0 with ERROR_CLASS_ALREADY_EXISTS sometimes.
		// For simplicity we ignore that case; CreateWindowExW will still work.
		_ = err
	}

	// Create an invisible window
	h, _, err2 := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(wstr("CapsLangSwitch"))),
		0,
		uintptr(CW_USEDEFAULT), uintptr(CW_USEDEFAULT),
		uintptr(CW_USEDEFAULT), uintptr(CW_USEDEFAULT),
		0, 0,
		hMod,
		0,
	)
	if h == 0 {
		return err2
	}

	hwnd = windows.Handle(h)
	return nil
}

func installHook() error {
	hMod, _, _ := procGetModuleHandleW.Call(0)
	r, _, err := procSetWindowsHookExW.Call(
		uintptr(WH_KEYBOARD_LL),
		hookProc,
		hMod,
		0,
	)
	if r == 0 {
		return err
	}
	hHook = windows.Handle(r)
	return nil
}

func messageLoop() {
	var msg MSG
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 { // 0=WM_QUIT, -1=error
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func main() {
	runtime.LockOSThread()

	if err := createHiddenWindow(); err != nil {
		panic(err)
	}
	buildMenus()

	if err := addTrayIcon(); err != nil {
		panic(err)
	}
	if err := installHook(); err != nil {
		panic(err)
	}

	messageLoop()
}
