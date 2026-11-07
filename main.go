package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Device struct {
	Device     string
	Level      float64
	DeviceType int
	Charging   *bool `json:"Charging"`
	IsCharging *bool `json:"IsCharging"`
}
type API struct {
	Code int `json:"code"`
	Data struct {
		Battery map[string]Device `json:"battery"`
	} `json:"data"`
}
type Message struct {
	Event   string          `json:"event"`
	Context string          `json:"context"`
	Payload json.RawMessage `json:"payload"`
}
type Instance struct {
	Serial       string `json:"serial"`
	Endpoint     string `json:"endpoint"`
	Display      string `json:"display"`
	ShowName     *bool  `json:"showName"`
	ShowType     *bool  `json:"showType"`
	BatteryFont  int    `json:"batteryFont"`
	LabelFont    int    `json:"labelFont"`
	NamePosition string `json:"namePosition"`
	TypePosition string `json:"typePosition"`
	ShowCharging *bool  `json:"showCharging"`
}
type BatteryHistory struct {
	Level        float64
	LastIncrease time.Time
}
type Plugin struct {
	conn        net.Conn
	reader      *bufio.Reader
	mu          sync.Mutex
	state       sync.Mutex
	instances   map[string]Instance
	battery     map[string]Device
	endpoints   map[string]map[string]Device
	connections map[string]bool
	history     map[string]BatteryHistory
	connected   bool
}

func (p *Plugin) send(v any) {
	b, e := json.Marshal(v)
	if e != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer p.conn.SetWriteDeadline(time.Time{})
	frame := []byte{0x81}
	n := len(b)
	if n < 126 {
		frame = append(frame, byte(n))
	} else if n <= 65535 {
		frame = append(frame, 126, byte(n>>8), byte(n))
	} else {
		frame = append(frame, 127)
		var sz [8]byte
		binary.BigEndian.PutUint64(sz[:], uint64(n))
		frame = append(frame, sz[:]...)
	}
	frame = append(frame, b...)
	_, _ = p.conn.Write(frame)
}
func (p *Plugin) emit(event, ctx string, payload any) {
	p.send(map[string]any{"event": event, "context": ctx, "payload": payload})
}
func (p *Plugin) read() ([]byte, error) {
	for {
		a, e := p.reader.ReadByte()
		if e != nil {
			return nil, e
		}
		b, e := p.reader.ReadByte()
		if e != nil {
			return nil, e
		}
		n := uint64(b & 127)
		if n == 126 {
			var z [2]byte
			if _, e = io.ReadFull(p.reader, z[:]); e != nil {
				return nil, e
			}
			n = uint64(binary.BigEndian.Uint16(z[:]))
		} else if n == 127 {
			var z [8]byte
			if _, e = io.ReadFull(p.reader, z[:]); e != nil {
				return nil, e
			}
			n = binary.BigEndian.Uint64(z[:])
		}
		if n > 8*1024*1024 {
			return nil, fmt.Errorf("frame too large")
		}
		var mask [4]byte
		if b&128 != 0 {
			if _, e = io.ReadFull(p.reader, mask[:]); e != nil {
				return nil, e
			}
		}
		data := make([]byte, int(n))
		if _, e = io.ReadFull(p.reader, data); e != nil {
			return nil, e
		}
		if b&128 != 0 {
			for i := range data {
				data[i] ^= mask[i%4]
			}
		}
		switch a & 15 {
		case 1, 2:
			return data, nil
		case 8:
			return nil, io.EOF
		case 9:
			p.pong(data)
		}
	}
}
func (p *Plugin) pong(b []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	frame := []byte{0x8a, byte(0x80 | len(b))}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	frame = append(frame, mask[:]...)
	for i, v := range b {
		frame = append(frame, v^mask[i%4])
	}
	_, _ = p.conn.Write(frame)
}
func connect(port string) (net.Conn, *bufio.Reader, error) {
	conn, e := net.DialTimeout("tcp", "127.0.0.1:"+port, 5*time.Second)
	if e != nil {
		return nil, nil, e
	}
	var nonce [16]byte
	_, _ = rand.Read(nonce[:])
	key := base64.StdEncoding.EncodeToString(nonce[:])
	req := fmt.Sprintf("GET / HTTP/1.1\r\nHost: 127.0.0.1:%s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", port, key)
	if _, e = conn.Write([]byte(req)); e != nil {
		conn.Close()
		return nil, nil, e
	}
	r := bufio.NewReader(conn)
	line, e := r.ReadString('\n')
	if e != nil || !strings.Contains(line, "101") {
		conn.Close()
		return nil, nil, fmt.Errorf("websocket handshake failed: %s", line)
	}
	headers := map[string]string{}
	for {
		line, e = r.ReadString('\n')
		if e != nil {
			conn.Close()
			return nil, nil, e
		}
		if line == "\r\n" {
			break
		}
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) == 2 {
			headers[strings.ToLower(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	expected := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if headers["sec-websocket-accept"] != base64.StdEncoding.EncodeToString(expected[:]) {
		conn.Close()
		return nil, nil, fmt.Errorf("websocket accept mismatch")
	}
	return conn, r, nil
}
func (p *Plugin) sendMasked(v any) {
	b, e := json.Marshal(v)
	if e != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	frame := []byte{0x81}
	n := len(b)
	if n < 126 {
		frame = append(frame, 0x80|byte(n))
	} else if n <= 65535 {
		frame = append(frame, 0x80|126, byte(n>>8), byte(n))
	} else {
		frame = append(frame, 0x80|127)
		var z [8]byte
		binary.BigEndian.PutUint64(z[:], uint64(n))
		frame = append(frame, z[:]...)
	}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	frame = append(frame, mask[:]...)
	for i, v := range b {
		frame = append(frame, v^mask[i%4])
	}
	_, _ = p.conn.Write(frame)
}
func (p *Plugin) event(ev, ctx string, payload any) {
	p.sendMasked(map[string]any{"event": ev, "context": ctx, "payload": payload})
}
func (p *Plugin) inspector(ctx string) {
	p.state.Lock()
	inst := p.instances[ctx]
	endpoint := normalizeEndpoint(inst.Endpoint)
	b := p.endpoints[endpoint]
	connected := p.connections[endpoint]
	p.state.Unlock()
	p.event("sendToPropertyInspector", ctx, map[string]any{"battery": b, "connected": connected})
}
func esc(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
func normalizeEndpoint(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "127.0.0.1:27003"
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "http" || u.Hostname() == "" || u.User != nil {
		return "http://127.0.0.1:27003/api/systray"
	}
	if u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), "27003")
	}
	u.Path = "/api/systray"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
func deviceType(d Device) string {
	switch d.DeviceType {
	case 0:
		return "KEYBOARD"
	case 1:
		return "MOUSE"
	case 2:
		return "HEADSET"
	default:
		return "DEVICE"
	}
}
func clamp(v, lo, hi, fallback int) int {
	if v == 0 {
		return fallback
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
func draw(d Device, valid bool, inst Instance, recentlyIncreased bool) string {
	bg := "#121923"
	family := "sans-serif"
	if !valid {
		content := `<text x="72" y="74" text-anchor="middle" dominant-baseline="middle" font-size="21" font-weight="bold" fill="#e2e8f0" font-family="sans-serif"><tspan x="72" dy="-11">Device</tspan><tspan x="72" dy="25">Offline</tspan></text>`
		svg := `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="144" height="144" viewBox="0 0 144 144"><rect width="144" height="144" rx="15" fill="` + bg + `"/>` + content + `</svg>`
		return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
	}
	pct := int(d.Level + 0.5)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	color := "#22c55e"
	if pct < 20 {
		color = "#ef4444"
	} else if pct <= 40 {
		color = "#f59e0b"
	}
	showName, showType := true, false
	switch inst.Display {
	case "battery":
		showName = false
	case "type":
		showName = false
		showType = true
	}
	if inst.ShowName != nil {
		showName = *inst.ShowName
	}
	if inst.ShowType != nil {
		showType = *inst.ShowType
	}
	batteryFont := clamp(inst.BatteryFont, 18, 54, 43)
	labelFont := clamp(inst.LabelFont, 8, 20, 15)
	type labelLine struct{ text, position string }
	labels := []labelLine{}
	position := func(s string) string {
		if s == "above" {
			return "above"
		}
		return "below"
	}
	if showName {
		labels = append(labels, labelLine{d.Device, position(inst.NamePosition)})
	}
	if showType {
		labels = append(labels, labelLine{deviceType(d), position(inst.TypePosition)})
	}
	counts := map[string]int{"above": 0, "below": 0}
	for _, l := range labels {
		counts[l.position]++
	}
	indexes := map[string]int{"above": 0, "below": 0}
	labelSvg := ""
	for _, line := range labels {
		label := line.text
		maxChars := 22
		if labelFont > 14 {
			maxChars = 17
		}
		if labelFont > 17 {
			maxChars = 14
		}
		rr := []rune(label)
		if len(rr) > maxChars {
			label = string(rr[:maxChars-1]) + "…"
		}
		i := indexes[line.position]
		indexes[line.position]++
		y := 122
		if line.position == "above" {
			y = 35
			if counts["above"] > 1 {
				y = 17 + i*21
			}
		} else if counts["below"] > 1 {
			y = 116 + i*21
		}
		labelSvg += fmt.Sprintf(`<text x="72" y="%d" text-anchor="middle" font-size="%d" fill="#e2e8f0" font-family="%s">%s</text>`, y, labelFont, family, esc(label))
	}
	arrow := ""
	showCharging := inst.ShowCharging == nil || *inst.ShowCharging
	// A black chevron group sits inside the filled part of the battery bar.
	// Charging is only inferred from a recent increase in the reported percentage.
	if showCharging && recentlyIncreased && pct > 0 && pct < 100 {
		filled := float64(114*pct) / 100.0
		// Use fewer chevrons on short fills, keeping all marks inside the colored area.
		count := 3
		if filled < 37 {
			count = 2
		}
		if filled < 23 {
			count = 1
		}
		if filled >= 10 {
			for i := count - 1; i >= 0; i-- {
				x := 15 + filled - 5 - float64(i)*8
				arrow += fmt.Sprintf(`<path d="M%.1f 92 l3 2.5 -3 2.5" fill="none" stroke="#000000" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>`, x)
			}
		}
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="144" height="144" viewBox="0 0 144 144"><rect width="144" height="144" rx="15" fill="%s"/><text x="72" y="77" text-anchor="middle" font-size="%d" font-weight="bold" fill="white" font-family="%s">%d%%</text><rect x="15" y="90" width="114" height="9" rx="4" fill="#344154"/><rect x="15" y="90" width="%d" height="9" rx="4" fill="%s"/>%s%s</svg>`, bg, batteryFont, family, pct, 114*pct/100, color, arrow, labelSvg)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}
func (p *Plugin) update() {
	p.state.Lock()
	instances := make(map[string]Instance, len(p.instances))
	for k, v := range p.instances {
		instances[k] = v
	}
	batteries := make(map[string]map[string]Device, len(p.endpoints))
	for k, v := range p.endpoints {
		batteries[k] = v
	}
	history := make(map[string]BatteryHistory, len(p.history))
	for k, v := range p.history {
		history[k] = v
	}
	connected := make(map[string]bool, len(p.connections))
	for k, v := range p.connections {
		connected[k] = v
	}
	p.state.Unlock()
	for ctx, inst := range instances {
		endpoint := normalizeEndpoint(inst.Endpoint)
		d, ok := batteries[endpoint][inst.Serial]
		if inst.Serial == "" {
			d = Device{Device: "SELECT DEVICE"}
		} else if !ok {
			d = Device{Device: "NOT AVAILABLE"}
		}
		p.event("setImage", ctx, map[string]any{"image": draw(d, connected[endpoint] && ok && inst.Serial != "", inst, !history[endpoint+"|"+inst.Serial].LastIncrease.IsZero() && time.Since(history[endpoint+"|"+inst.Serial].LastIncrease) < 10*time.Minute), "target": 0})
		p.event("setTitle", ctx, map[string]any{"title": "", "target": 0})
	}
}
func (p *Plugin) refresh() {
	p.state.Lock()
	endpoints := map[string]bool{}
	for _, inst := range p.instances {
		endpoints[normalizeEndpoint(inst.Endpoint)] = true
	}
	p.state.Unlock()
	client := http.Client{Timeout: 4 * time.Second}
	for endpoint := range endpoints {
		var a API
		res, e := client.Get(endpoint)
		if e == nil {
			if res.StatusCode == 200 {
				e = json.NewDecoder(io.LimitReader(res.Body, 1024*1024)).Decode(&a)
			} else {
				e = fmt.Errorf("status %d", res.StatusCode)
			}
			res.Body.Close()
		}
		ok := e == nil && a.Code == 200 && a.Data.Battery != nil
		p.state.Lock()
		p.connections[endpoint] = ok
		if ok {
			now := time.Now()
			for serial, d := range a.Data.Battery {
				key := endpoint + "|" + serial
				previous, seen := p.history[key]
				if seen && d.Level > previous.Level {
					previous.LastIncrease = now
				}
				previous.Level = d.Level
				p.history[key] = previous
			}
			// Missing devices must not keep an old charging indication.
			for key := range p.history {
				if strings.HasPrefix(key, endpoint+"|") {
					serial := strings.TrimPrefix(key, endpoint+"|")
					if _, present := a.Data.Battery[serial]; !present {
						delete(p.history, key)
					}
				}
			}
			p.endpoints[endpoint] = a.Data.Battery
		} else {
			// A reconnect must not interpret an old value as a fresh increase.
			for key := range p.history {
				if strings.HasPrefix(key, endpoint+"|") {
					delete(p.history, key)
				}
			}
		}
		p.state.Unlock()
	}
	p.update()
	p.state.Lock()
	contexts := make([]string, 0, len(p.instances))
	for ctx := range p.instances {
		contexts = append(contexts, ctx)
	}
	p.state.Unlock()
	for _, ctx := range contexts {
		p.inspector(ctx)
	}
}
func (p *Plugin) handle(b []byte) {
	var m Message
	if json.Unmarshal(b, &m) != nil {
		return
	}
	switch m.Event {
	case "willAppear", "didReceiveSettings":
		var payload struct {
			Settings Instance `json:"settings"`
		}
		_ = json.Unmarshal(m.Payload, &payload)
		p.state.Lock()
		p.instances[m.Context] = payload.Settings
		p.state.Unlock()
		p.update()
		if m.Event == "willAppear" {
			go p.refresh()
		}
	case "willDisappear":
		p.state.Lock()
		delete(p.instances, m.Context)
		p.state.Unlock()
	case "propertyInspectorDidAppear":
		p.inspector(m.Context)
		go p.refresh()
	case "sendToPlugin":
		go p.refresh()
	}
}
func main() {
	log.SetOutput(os.Stderr)
	args := map[string]string{}
	for i := 1; i+1 < len(os.Args); i++ {
		if strings.HasPrefix(os.Args[i], "-") {
			args[strings.TrimPrefix(os.Args[i], "-")] = os.Args[i+1]
			i++
		}
	}
	port := args["port"]
	if port == "" {
		log.Fatal("missing -port")
	}
	conn, r, e := connect(port)
	if e != nil {
		log.Fatal(e)
	}
	defer conn.Close()
	p := &Plugin{conn: conn, reader: r, instances: map[string]Instance{}, battery: map[string]Device{}, endpoints: map[string]map[string]Device{}, connections: map[string]bool{}, history: map[string]BatteryHistory{}}
	event := args["registerEvent"]
	if event == "" {
		event = "registerPlugin"
	}
	p.sendMasked(map[string]any{"event": event, "uuid": args["pluginUUID"]})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			p.refresh()
		}
	}()
	for {
		msg, e := p.read()
		if e != nil {
			log.Print(e)
			return
		}
		p.handle(msg)
	}
}
