//go:build novoice && windows

// wasapi_devices_windows.go enumerates WASAPI audio endpoints via Windows COM
// using pure Go (no CGO). Works in stub builds on Windows Vista+.

package client

import (
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ── COM GUIDs ─────────────────────────────────────────────────────────────────

var (
	// CLSID_MMDeviceEnumerator = {BCDE0395-E52F-467C-8E3D-C4579291692E}
	clsidMMDeviceEnum = windows.GUID{
		Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C,
		Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E},
	}
	// IID_IMMDeviceEnumerator = {A95664D2-9614-4F35-A746-DE8DB63617E6}
	iidMMDeviceEnum = windows.GUID{
		Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35,
		Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6},
	}
)

// PKEY_Device_FriendlyName = {A45C254E-DF1C-4EFD-8020-67D146A850E0}, pid=14
// C layout: GUID(16 bytes) + DWORD(4 bytes) = 20 bytes.
type wasapiPropKey struct {
	GUID windows.GUID
	PID  uint32
}

var wasapiFriendlyNameKey = wasapiPropKey{
	GUID: windows.GUID{
		Data1: 0xA45C254E, Data2: 0xDF1C, Data3: 0x4EFD,
		Data4: [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0},
	},
	PID: 14,
}

// wasapiPropVariant mirrors the first 24 bytes of PROPVARIANT on 64-bit Windows.
// Header: vt(2)+reserved×3(6) = 8 bytes. Union: ptr(8)+pad(8) = 16 bytes.
// For VT_LPWSTR (31) the union holds an unsafe.Pointer (= LPWSTR).
type wasapiPropVariant struct {
	VT        uint16
	Reserved1 uint16
	Reserved2 uint16
	Reserved3 uint16
	Val       unsafe.Pointer // LPWSTR for VT_LPWSTR
	pad       unsafe.Pointer // pad union to 16 bytes on 64-bit
}

const (
	wasapiECapture          = 0x00000001 // eCapture data-flow (microphone)
	wasapiERender           = 0x00000000 // eRender data-flow (speaker)
	wasapiDeviceStateActive = 0x00000001
	wasapiSTGM_READ         = 0
	wasapiVT_LPWSTR         = 31
	wasapiCLSCTX_ALL        = 0x17
	wasapiCOINIT_STA        = 0x2 // COINIT_APARTMENTTHREADED
)

// ── Lazy-loaded DLL procs ─────────────────────────────────────────────────────

var (
	modOle32          = windows.NewLazySystemDLL("ole32.dll")
	procWCoInitEx     = modOle32.NewProc("CoInitializeEx")
	procWCoUninit     = modOle32.NewProc("CoUninitialize")
	procWCoCreateInst = modOle32.NewProc("CoCreateInstance")
	procWCoTaskFree   = modOle32.NewProc("CoTaskMemFree")
)

// ── Vtable helpers ────────────────────────────────────────────────────────────
//
// COM interface pointers point to a vtable (array of function pointers).
// iface → *vtable → [method0, method1, ...]
// All helpers accept unsafe.Pointer for the COM interface to avoid the
// uintptr→unsafe.Pointer roundtrip that go vet flags.

// wVtbl returns the function pointer for vtable method idx.
func wVtbl(iface unsafe.Pointer, idx int) uintptr {
	vtblPtr := *(*unsafe.Pointer)(iface) // load vtable pointer — blessed: *T1→*T2
	return *(*uintptr)(unsafe.Add(vtblPtr, uintptr(idx)*unsafe.Sizeof(uintptr(0))))
}

// wRelease calls IUnknown::Release (vtable index 2).
func wRelease(iface unsafe.Pointer) {
	syscall.Syscall(wVtbl(iface, 2), 1, uintptr(iface), 0, 0) //nolint:errcheck
}

// ── Typed wrappers for each COM call signature ────────────────────────────────
// Using specific wrappers avoids storing unsafe.Pointer as uintptr.

// EnumAudioEndpoints(eDataFlow, dwStateMask, **IMMDeviceCollection) HRESULT
func wEnumAudioEndpoints(iface unsafe.Pointer, dataFlow, stateMask uintptr, ppColl *unsafe.Pointer) uintptr {
	r, _, _ := syscall.Syscall6(wVtbl(iface, 3), 4,
		uintptr(iface), dataFlow, stateMask, uintptr(unsafe.Pointer(ppColl)), 0, 0)
	return r
}

// GetCount(*UINT) HRESULT — IMMDeviceCollection vtable index 3
func wGetCount(iface unsafe.Pointer, pCount *uint32) uintptr {
	r, _, _ := syscall.Syscall(wVtbl(iface, 3), 2,
		uintptr(iface), uintptr(unsafe.Pointer(pCount)), 0)
	return r
}

// Item(UINT, **IMMDevice) HRESULT — IMMDeviceCollection vtable index 4
func wItem(iface unsafe.Pointer, n uintptr, ppDev *unsafe.Pointer) uintptr {
	r, _, _ := syscall.Syscall(wVtbl(iface, 4), 3,
		uintptr(iface), n, uintptr(unsafe.Pointer(ppDev)))
	return r
}

// GetId(**LPWSTR) HRESULT — IMMDevice vtable index 5
func wGetId(iface unsafe.Pointer, ppStr *unsafe.Pointer) uintptr {
	r, _, _ := syscall.Syscall(wVtbl(iface, 5), 2,
		uintptr(iface), uintptr(unsafe.Pointer(ppStr)), 0)
	return r
}

// OpenPropertyStore(DWORD, **IPropertyStore) HRESULT — IMMDevice vtable index 4
func wOpenPropStore(iface unsafe.Pointer, access uintptr, ppStore *unsafe.Pointer) uintptr {
	r, _, _ := syscall.Syscall(wVtbl(iface, 4), 3,
		uintptr(iface), access, uintptr(unsafe.Pointer(ppStore)))
	return r
}

// GetValue(*PROPERTYKEY, *PROPVARIANT) HRESULT — IPropertyStore vtable index 5
func wGetValue(iface unsafe.Pointer, key *wasapiPropKey, pv *wasapiPropVariant) uintptr {
	r, _, _ := syscall.Syscall(wVtbl(iface, 5), 3,
		uintptr(iface),
		uintptr(unsafe.Pointer(key)),
		uintptr(unsafe.Pointer(pv)))
	return r
}

// ── Public API ────────────────────────────────────────────────────────────────

// GetAudioDevices enumerates active WASAPI input and output devices via COM.
// It pins the calling goroutine to an OS thread for COM STA compatibility.
func GetAudioDevices() (inputs, outputs []AudioDevice, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// CoInitializeEx — S_OK (0) or S_FALSE (1) mean initialized; ignore errors.
	procWCoInitEx.Call(0, wasapiCOINIT_STA)
	defer procWCoUninit.Call()

	var enumerator unsafe.Pointer
	hr, _, _ := procWCoCreateInst.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnum)),
		0, // pUnkOuter = NULL
		wasapiCLSCTX_ALL,
		uintptr(unsafe.Pointer(&iidMMDeviceEnum)),
		uintptr(unsafe.Pointer(&enumerator)),
	)
	if hr != 0 || enumerator == nil {
		return nil, nil, nil // COM unavailable — return empty, not an error
	}
	defer wRelease(enumerator)

	inputs = wasapiEnumEndpoints(enumerator, wasapiECapture)
	outputs = wasapiEnumEndpoints(enumerator, wasapiERender)
	return inputs, outputs, nil
}

// ── Internal enumeration ──────────────────────────────────────────────────────

func wasapiEnumEndpoints(enumerator unsafe.Pointer, dataFlow uintptr) []AudioDevice {
	var collection unsafe.Pointer
	hr := wEnumAudioEndpoints(enumerator, dataFlow, wasapiDeviceStateActive, &collection)
	if hr != 0 || collection == nil {
		return nil
	}
	defer wRelease(collection)
	return wasapiCollectDevices(collection)
}

func wasapiCollectDevices(collection unsafe.Pointer) []AudioDevice {
	var count uint32
	if wGetCount(collection, &count) != 0 || count == 0 {
		return nil
	}

	devs := make([]AudioDevice, 0, count)
	for i := uint32(0); i < count; i++ {
		var device unsafe.Pointer
		if wItem(collection, uintptr(i), &device) != 0 || device == nil {
			continue
		}
		id, name := wasapiDeviceInfo(device)
		wRelease(device)
		if name == "" {
			name = id
		}
		if id != "" || name != "" {
			devs = append(devs, AudioDevice{ID: id, Name: name})
		}
	}
	return devs
}

func wasapiDeviceInfo(device unsafe.Pointer) (id, name string) {
	// Endpoint ID string — COM-allocated, free with CoTaskMemFree.
	var idPtr unsafe.Pointer
	if wGetId(device, &idPtr) == 0 && idPtr != nil {
		id = windows.UTF16PtrToString((*uint16)(idPtr))
		procWCoTaskFree.Call(uintptr(idPtr))
	}

	// Property store → friendly display name
	var propStore unsafe.Pointer
	if wOpenPropStore(device, wasapiSTGM_READ, &propStore) == 0 && propStore != nil {
		name = wasapiReadFriendlyName(propStore)
		wRelease(propStore)
	}
	return id, name
}

func wasapiReadFriendlyName(propStore unsafe.Pointer) string {
	var pv wasapiPropVariant
	hr := wGetValue(propStore, &wasapiFriendlyNameKey, &pv)
	if hr != 0 || pv.VT != wasapiVT_LPWSTR || pv.Val == nil {
		return ""
	}
	name := windows.UTF16PtrToString((*uint16)(pv.Val))
	// For VT_LPWSTR, PropVariantClear just calls CoTaskMemFree on the pointer.
	procWCoTaskFree.Call(uintptr(pv.Val))
	return name
}
