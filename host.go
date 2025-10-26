// host.go — Host completo (Windows)
// Dependências:
//   go get github.com/gorilla/websocket
//   go get github.com/kbinani/screenshot
//   go get github.com/google/uuid
//   go get github.com/atotto/clipboard
//
// Compilar (local):
//   GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o host.exe host.go
//
// Uso:
//   Edite serverDefault para ws://localhost:3000 ou ws://SEU_VPS:3000

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/atotto/clipboard"
	"github.com/gorilla/websocket"
	"github.com/google/uuid"
	"github.com/kbinani/screenshot"
)

// ---------------- Windows API bindings ----------------
var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW  = user32.NewProc("MessageBoxW")
	procSetCursorPos = user32.NewProc("SetCursorPos")
	procMouseEvent   = user32.NewProc("mouse_event")
	procKeybdEvent   = user32.NewProc("keybd_event")
	procVkKeyScanA   = user32.NewProc("VkKeyScanA")
)

const (
	// mouse flags
	MOUSEEVENTF_MOVE      = 0x0001
	MOUSEEVENTF_LEFTDOWN  = 0x0002
	MOUSEEVENTF_LEFTUP    = 0x0004
	MOUSEEVENTF_RIGHTDOWN = 0x0008
	MOUSEEVENTF_RIGHTUP   = 0x0010
	MOUSEEVENTF_WHEEL     = 0x0800

	// keyboard flags
	KEYEVENTF_EXTENDEDKEY = 0x0001
	KEYEVENTF_KEYUP       = 0x0002

	WHEEL_DELTA = 120
)

// MessageBox (simple)
func MessageBox(title, text string) {
	tt, _ := syscall.UTF16PtrFromString(title)
	tx, _ := syscall.UTF16PtrFromString(text)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(tx)), uintptr(unsafe.Pointer(tt)), 0)
}

// mouse/keyboard wrappers
func setCursorPos(x, y int) {
	procSetCursorPos.Call(uintptr(x), uintptr(y))
}

func mouseClick(button string) {
	if strings.ToLower(button) == "right" {
		procMouseEvent.Call(uintptr(MOUSEEVENTF_RIGHTDOWN), 0, 0, 0, 0)
		time.Sleep(8 * time.Millisecond)
		procMouseEvent.Call(uintptr(MOUSEEVENTF_RIGHTUP), 0, 0, 0, 0)
	} else {
		procMouseEvent.Call(uintptr(MOUSEEVENTF_LEFTDOWN), 0, 0, 0, 0)
		time.Sleep(8 * time.Millisecond)
		procMouseEvent.Call(uintptr(MOUSEEVENTF_LEFTUP), 0, 0, 0, 0)
	}
}

func mouseDoubleClick() {
	mouseClick("left")
	time.Sleep(50 * time.Millisecond)
	mouseClick("left")
}

func mouseWheel(delta int32) {
	// delta in steps; Windows expects multiples of WHEEL_DELTA
	procMouseEvent.Call(uintptr(MOUSEEVENTF_WHEEL), 0, 0, uintptr(delta*WHEEL_DELTA), 0)
}

func keyDown(vk byte) {
	procKeybdEvent.Call(uintptr(vk), 0, 0, 0)
}

func keyUp(vk byte) {
	procKeybdEvent.Call(uintptr(vk), 0, KEYEVENTF_KEYUP, 0)
}

func keyTap(vk byte) {
	keyDown(vk)
	time.Sleep(10 * time.Millisecond)
	keyUp(vk)
}

func vkFromKeyName(k string) byte {
	k = strings.ToLower(k)
	switch k {
	case "shift":
		return 0x10
	case "control", "ctrl":
		return 0x11
	case "alt":
		return 0x12
	case "enter":
		return 0x0D
	case "backspace":
		return 0x08
	case "tab":
		return 0x09
	case "escape", "esc":
		return 0x1B
	case "left", "arrowleft":
		return 0x25
	case "up", "arrowup":
		return 0x26
	case "right", "arrowright":
		return 0x27
	case "down", "arrowdown":
		return 0x28
	case "delete":
		return 0x2E
	case "space":
		return 0x20
	}
	// fallback: use VkKeyScanA for ASCII char
	if len(k) > 0 {
		c := k[0]
		ret, _, _ := procVkKeyScanA.Call(uintptr(c))
		vk := byte(ret & 0xff)
		return vk
	}
	return 0
}

// ---------------- Protocol structures ----------------

type RegisterMsg struct {
	Type string `json:"type"`
	Id   string `json:"id,omitempty"`
}

type ControlPayload struct {
	Type   string                 `json:"type"`             // "mouse" or "key" or "clipboard"
	Action string                 `json:"action,omitempty"` // "move","click","wheel","tap","down","up","set","get"
	X      float64                `json:"x,omitempty"`
	Y      float64                `json:"y,omitempty"`
	Button string                 `json:"button,omitempty"`
	Delta  float64                `json:"delta,omitempty"`
	Key    string                 `json:"key,omitempty"`
	Text   string                 `json:"text,omitempty"` // for clipboard set
	Extra  map[string]interface{} `json:"extra,omitempty"`
}

type ControlMsg struct {
	Type    string         `json:"type"` // "control" or other
	HostId  string         `json:"hostId,omitempty"`
	Payload ControlPayload `json:"payload,omitempty"`
}

// ---------------- Screen capture ----------------

func captureAllMonitorsJPEG(quality int) ([]byte, error) {
	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return nil, fmt.Errorf("no displays detected")
	}
	// build union rectangle
	var full image.Rectangle
	for i := 0; i < n; i++ {
		full = full.Union(screenshot.GetDisplayBounds(i))
	}
	// create image and draw each display into correct offset
	img := image.NewRGBA(full)
	for i := 0; i < n; i++ {
		rect := screenshot.GetDisplayBounds(i)
		bi, err := screenshot.CaptureRect(rect)
		if err != nil {
			return nil, err
		}
		// copy pixels
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				img.Set(x, y, bi.At(x-rect.Min.X, y-rect.Min.Y))
			}
		}
	}
	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---------------- Main behavior ----------------

func processControl(p ControlPayload) {
	switch p.Type {
	case "mouse":
		switch p.Action {
		case "move":
			setCursorPos(int(p.X), int(p.Y))
		case "click":
			btn := p.Button
			if btn == "" {
				btn = "left"
			}
			mouseClick(btn)
		case "double":
			mouseDoubleClick()
		case "wheel":
			mouseWheel(int32(p.Delta))
		}
	case "key":
		switch p.Action {
		case "tap":
			vk := vkFromKeyName(p.Key)
			if vk != 0 {
				keyTap(vk)
			}
		case "down":
			vk := vkFromKeyName(p.Key)
			if vk != 0 {
				keyDown(vk)
			}
		case "up":
			vk := vkFromKeyName(p.Key)
			if vk != 0 {
				keyUp(vk)
			}
		}
	case "clipboard":
		switch p.Action {
		case "set":
			clipboard.WriteAll(p.Text)
		case "get":
			// reading clipboard and optionally send back would require server/client support
		}
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	// flags
	serverDefault := "ws://localhost:3000"
	server := flag.String("server", serverDefault, "WebSocket server URL")
	fps := flag.Int("fps", 8, "frames per second to send")
	quality := flag.Int("quality", 60, "jpeg quality 1-100")
	flag.Parse()

	// build URL
	u, err := url.Parse(*server)
	if err != nil {
		log.Fatalf("invalid server url: %v", err)
	}
	log.Printf("Connecting to signaling server: %s\n", u.String())

	// connect to websocket (with basic retry)
	var conn *websocket.Conn
	for attempt := 1; attempt <= 6; attempt++ {
		conn, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
		if err == nil {
			break
		}
		log.Printf("dial attempt %d failed: %v — retrying in 3s\n", attempt, err)
		time.Sleep(3 * time.Second)
	}
	if conn == nil {
		log.Fatalf("could not connect to server: %v", err)
	}
	defer conn.Close()
	log.Println("Connected to server")

	// generate host id and register
	hostID := uuid.New().String()
	reg := RegisterMsg{Type: "register_host", Id: hostID}
	if err := conn.WriteJSON(reg); err != nil {
		log.Printf("error sending register: %v", err)
	}

	// copy host id to clipboard and popup
	if err := clipboard.WriteAll(hostID); err == nil {
		go MessageBox("Host ID copiado", "Host ID: "+hostID)
		log.Printf("Host ID: %s (copied to clipboard)\n", hostID)
	} else {
		log.Printf("Host ID: %s (clipboard write failed: %v)\n", hostID, err)
	}

	// channels & state
	stopCapture := make(chan struct{})
	done := make(chan struct{})
	controlChan := make(chan ControlPayload, 50)

	// reader routine: handle incoming messages (text control messages)
	go func() {
		defer close(done)
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("read error: %v\n", err)
				return
			}
			if mt == websocket.TextMessage {
				var cmsg ControlMsg
				if err := json.Unmarshal(msg, &cmsg); err != nil {
					log.Printf("invalid json message: %v\n", err)
					continue
				}
				if cmsg.Type == "control" {
					controlChan <- cmsg.Payload
				}
			} else {
				// ignore binary for now
			}
		}
	}()

	// capture & send frames goroutine
	go func() {
		ticker := time.NewTicker(time.Duration(1000/(*fps)) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopCapture:
				log.Println("stopping capture")
				return
			case <-ticker.C:
				frame, err := captureAllMonitorsJPEG(*quality)
				if err != nil {
					log.Printf("capture error: %v\n", err)
					continue
				}
				// send as binary frame
				if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
					log.Printf("write frame error: %v\n", err)
					return
				}
			}
		}
	}()

	// control processing goroutine
	go func() {
		for p := range controlChan {
			processControl(p)
		}
	}()

	// handle system signals (so the console doesn't close immediately)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	log.Println("Host running. Press Ctrl+C (or close window) to stop.")
	<-sig

	// cleanup
	close(stopCapture)
	conn.Close()
	// give goroutines a little time
	time.Sleep(300 * time.Millisecond)
	log.Println("Exiting host.")
}
