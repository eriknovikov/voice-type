package voicetype

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	ibusService         = "org.freedesktop.IBus"
	ibusPath            = dbus.ObjectPath("/org/freedesktop/IBus")
	ibusFactoryPath     = dbus.ObjectPath("/org/freedesktop/IBus/Factory")
	ibusEnginePath      = dbus.ObjectPath("/org/freedesktop/IBus/Engine/InsertPure")
	ibusEngineInterface = ibusService + ".Engine"
	ibusProperties      = "org.freedesktop.DBus.Properties"
	ibusSetupLimit      = 2 * time.Second
	// This is a scheduling/transport allowance, not an extra readiness window.
	insertPureSendGrace    = 100 * time.Millisecond
	insertPureCleanupLimit = 2 * time.Second
)

// These flat structs describe IBus 1.5 wire tuples. Embedding a common header
// would introduce an incorrect nested D-Bus struct. Empty arrays retain their
// element types. In particular, []interface{} is not a substitute for a tuple.
type ibusComponent struct {
	Type                                                                    string
	Attachments                                                             map[string]dbus.Variant
	Name, Description, Version, License, Author, Homepage, Exec, Textdomain string
	ObservedPaths, Engines                                                  []dbus.Variant
}

type ibusEngineDesc struct {
	Type                                                                                  string
	Attachments                                                                           map[string]dbus.Variant
	Name, LongName, Description, Language, License, Author, Icon, Layout                  string
	Rank                                                                                  uint32
	Hotkeys, Symbol, Setup, LayoutVariant, LayoutOption, Version, Textdomain, IconPropKey string
}

type ibusText struct {
	Type        string
	Attachments map[string]dbus.Variant
	Text        string
	Attributes  dbus.Variant
}

type ibusAttrList struct {
	Type        string
	Attachments map[string]dbus.Variant
	Attributes  []dbus.Variant
}

type ibusContentType struct{ Purpose, Hints uint32 }

func insertPureComponent(name string) dbus.Variant {
	engine := ibusEngineDesc{
		Type: "IBusEngineDesc", Name: name, LongName: "insert-pure",
		Description: "Temporary text insertion diagnostic", Language: "en",
		License: "MIT", Author: "Voice Type", Layout: "default",
	}
	return dbus.MakeVariant(ibusComponent{
		Type: "IBusComponent", Name: "org.freedesktop.IBus.InsertPure.Run" + strings.TrimPrefix(name, "insert-pure-"),
		Description: "Temporary text insertion diagnostic", Version: "1", License: "MIT", Author: "Voice Type",
		Engines: []dbus.Variant{dbus.MakeVariant(engine)},
	})
}

func insertPureWireText(text string) dbus.Variant {
	return dbus.MakeVariant(ibusText{Type: "IBusText", Text: text,
		Attributes: dbus.MakeVariant(ibusAttrList{Type: "IBusAttrList"})})
}

func ibusUnwrap(value any) any {
	for i := 0; i < 8; i++ {
		v, ok := value.(dbus.Variant)
		if !ok {
			return value
		}
		value = v.Value()
	}
	return nil
}

func ibusStore(value, target any) error {
	value = ibusUnwrap(value)
	if value == nil {
		return errors.New("invalid IBus value")
	}
	return dbus.Store([]any{value}, target)
}

func ibusEngineName(value any) (string, error) {
	var desc ibusEngineDesc
	if err := ibusStore(value, &desc); err != nil || desc.Type != "IBusEngineDesc" || desc.Name == "" {
		return "", errors.New("invalid IBus engine description")
	}
	return desc.Name, nil
}

// ibusAddress follows ibusshare.c. It deliberately does not scan other displays
// or use session-bus autolaunch. Neither metadata nor addresses reach errors.
func ibusAddress(env func(string) string, readFile func(string) ([]byte, error), alive func(int) bool) (string, error) {
	if address := env("IBUS_ADDRESS"); address != "" {
		return address, nil
	}
	path := env("IBUS_ADDRESS_FILE")
	if path == "" {
		machine := "machine-id"
		for _, p := range []string{"/var/lib/dbus/machine-id", "/etc/machine-id"} {
			if b, err := readFile(p); err == nil {
				machine = strings.TrimSpace(string(b))
				break
			}
		}
		host, display := "unix", "0"
		if wayland := env("WAYLAND_DISPLAY"); wayland != "" {
			display = wayland
		} else if x := env("DISPLAY"); x != "" {
			if h, d, ok := strings.Cut(x, ":"); ok {
				host, display = h, strings.SplitN(d, ".", 2)[0]
			} else {
				host = x
			}
			if host == "" {
				host = "unix"
			}
		}
		config := env("XDG_CONFIG_HOME")
		if config == "" {
			config = filepath.Join(env("HOME"), ".config")
		}
		if !filepath.IsAbs(config) {
			return "", errors.New("cannot locate the IBus configuration directory")
		}
		path = filepath.Join(config, "ibus", "bus", machine+"-"+host+"-"+display)
	}
	b, err := readFile(path)
	if err != nil {
		return "", errors.New("cannot read IBus address metadata for this display")
	}
	if len(b) > 64*1024 {
		return "", errors.New("IBus address metadata is too large")
	}
	var address string
	var pid int
	scanner := bufio.NewScanner(strings.NewReader(string(b)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if v, ok := strings.CutPrefix(line, "IBUS_ADDRESS="); ok {
			address = v
		}
		if v, ok := strings.CutPrefix(line, "IBUS_DAEMON_PID="); ok {
			pid, _ = strconv.Atoi(v)
		}
	}
	if scanner.Err() != nil || address == "" || pid <= 0 || !alive(pid) {
		return "", errors.New("IBus address metadata is invalid or stale")
	}
	return address, nil
}

func readIBusMetadata(path string) ([]byte, error) {
	// Nonblocking open also prevents an address-file override from hanging on
	// a FIFO. Only small regular files are valid discovery metadata.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return nil, errors.New("invalid IBus metadata file")
	}
	return io.ReadAll(io.LimitReader(f, 64*1024+1))
}

// Only local Unix transports are needed by IBus. Parsing them here permits
// DialContext and socket deadlines, including during authentication. No godbus
// session-bus helper (which can execute an autolaunch subprocess) is used.
func ibusUnixAddresses(address string) ([]string, error) {
	var result []string
	for _, entry := range strings.Split(address, ";") {
		kind, fields, ok := strings.Cut(entry, ":")
		if !ok || kind != "unix" {
			continue
		}
		values := map[string]string{}
		for _, field := range strings.Split(fields, ",") {
			k, v, ok := strings.Cut(field, "=")
			decoded, err := url.PathUnescape(v)
			if !ok || err != nil || strings.ContainsRune(decoded, 0) {
				return nil, errors.New("invalid IBus Unix address")
			}
			if _, exists := values[k]; exists {
				return nil, errors.New("invalid IBus Unix address")
			}
			values[k] = decoded
		}
		path, abstract := values["path"], values["abstract"]
		if path != "" && abstract == "" && filepath.IsAbs(path) {
			result = append(result, path)
		} else if abstract != "" && path == "" {
			result = append(result, "\x00"+abstract)
		} else {
			return nil, errors.New("invalid IBus Unix address")
		}
	}
	if len(result) == 0 {
		return nil, errors.New("IBus requires a local Unix bus address")
	}
	return result, nil
}

type ibusConnection struct {
	*dbus.Conn
	socket net.Conn
}

func (c *ibusConnection) close() {
	// Closing the socket first also releases a blocked godbus writer.
	_ = c.socket.Close()
	_ = c.Conn.Close()
}

func connectIBus(ctx context.Context, address string, interceptor dbus.Interceptor) (*ibusConnection, error) {
	paths, err := ibusUnixAddresses(address)
	if err != nil {
		return nil, err
	}
	var socket net.Conn
	for _, path := range paths {
		socket, err = (&net.Dialer{}).DialContext(ctx, "unix", path)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, errors.New("cannot connect to the IBus Unix socket")
	}
	stop := context.AfterFunc(ctx, func() { _ = socket.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = socket.SetDeadline(deadline)
	}
	conn, err := dbus.NewConn(socket, dbus.WithIncomingInterceptor(interceptor))
	if err != nil {
		_ = socket.Close()
		return nil, errors.New("cannot create the IBus connection")
	}
	c := &ibusConnection{Conn: conn, socket: socket}
	if err = conn.Auth([]dbus.Auth{dbus.AuthExternal(strconv.Itoa(os.Getuid()))}); err == nil {
		err = conn.Hello()
	}
	if err != nil || ctx.Err() != nil {
		c.close()
		return nil, errors.New("IBus authentication or Hello failed")
	}
	if !stop() {
		c.close()
		return nil, errors.New("IBus connection deadline expired")
	}
	_ = socket.SetDeadline(time.Time{})
	return c, nil
}

func (c *ibusConnection) property(ctx context.Context, name string) (dbus.Variant, error) {
	var value dbus.Variant
	err := c.Object(ibusService, ibusPath).CallWithContext(ctx, ibusProperties+".Get", 0, ibusService, name).Store(&value)
	return value, err
}

var errIBusNoGlobalEngine = errors.New("IBus has no global engine")

func (c *ibusConnection) globalEngine(ctx context.Context) (string, error) {
	v, err := c.property(ctx, "GlobalEngine")
	if err != nil {
		var remote dbus.Error
		if errors.As(err, &remote) && remote.Name == "org.freedesktop.DBus.Error.Failed" && len(remote.Body) == 1 && remote.Body[0] == "No global engine." {
			return "", errIBusNoGlobalEngine
		}
		return "", errors.New("cannot read the current IBus engine")
	}
	return ibusEngineName(v)
}

type insertPureEngine struct {
	mu                                     sync.Mutex
	name, owner                            string
	deadline                               time.Time
	created, destroyed, enabled, submitted bool
	focus, client                          string
	enabledAt, focusAt                     time.Time
	content                                ibusContentType
	propertyReplies                        chan *dbus.Message
	changed                                chan struct{}
}

func (e *insertPureEngine) trusted(sender string) bool {
	// IBus's private bus stamps its direct callbacks org.freedesktop.DBus.
	e.mu.Lock()
	defer e.mu.Unlock()
	return sender == "org.freedesktop.DBus" || (e.owner != "" && sender == e.owner)
}

func (e *insertPureEngine) authorize(sender dbus.Sender) *dbus.Error {
	if !e.trusted(string(sender)) {
		return dbus.NewError("org.freedesktop.DBus.Error.AccessDenied", []any{"only IBus can call this engine"})
	}
	return nil
}

// observe runs on godbus's reader, BEFORE it starts the exported-method
// goroutine. Exported callbacks only reply. A mutex in callbacks alone would
// not preserve wire order. Never make a bus call from this interceptor.
func (e *insertPureEngine) observe(msg *dbus.Message) {
	if msg.Type != dbus.TypeMethodCall {
		return
	}
	path, _ := msg.Headers[dbus.FieldPath].Value().(dbus.ObjectPath)
	sender, _ := msg.Headers[dbus.FieldSender].Value().(string)
	iface, _ := msg.Headers[dbus.FieldInterface].Value().(string)
	member, _ := msg.Headers[dbus.FieldMember].Value().(string)
	if (path != ibusEnginePath && path != ibusFactoryPath) || !e.trusted(sender) {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.destroyed {
		return
	}
	defer func() {
		select {
		case e.changed <- struct{}{}:
		default:
		}
	}()
	now := time.Now()
	if iface == ibusService+".Service" && member == "Destroy" && len(msg.Body) == 0 {
		e.destroyed = true
		return
	}
	if !e.created || path != ibusEnginePath {
		return
	}
	if iface == ibusProperties && e.queuePropertyReply(msg, member) {
		return
	}
	if iface == ibusProperties && member == "Set" && len(msg.Body) == 3 {
		if msg.Body[0] == ibusEngineInterface && msg.Body[1] == "ContentType" {
			var content ibusContentType
			if ibusStore(msg.Body[2], &content) == nil {
				e.content = content
			}
		}
		return
	}
	if iface != ibusEngineInterface {
		return
	}
	switch member {
	case "Enable":
		if len(msg.Body) == 0 {
			e.enabled, e.enabledAt = true, now
		}
	case "Disable":
		if len(msg.Body) == 0 {
			e.enabled = false
		}
	case "FocusInId":
		if len(msg.Body) == 2 {
			path, pOK := msg.Body[0].(string)
			client, cOK := msg.Body[1].(string)
			if pOK && cOK {
				e.focus, e.client, e.focusAt = path, client, now
			}
		}
	case "FocusOutId":
		if len(msg.Body) == 1 {
			if path, ok := msg.Body[0].(string); ok && path == e.focus {
				e.focus, e.client = "", ""
			}
		}
	case "FocusIn", "FocusOut":
		// Legacy FocusIn cannot prove the client's identity. Fail closed.
		if len(msg.Body) == 0 {
			e.focus, e.client = "", ""
		}
	}
}

func (e *insertPureEngine) eligible() bool {
	client := strings.ToLower(e.client)
	return e.created && !e.destroyed && e.enabled && !e.submitted &&
		!e.enabledAt.After(e.deadline) && !e.focusAt.After(e.deadline) &&
		dbus.ObjectPath(e.focus).IsValid() && strings.HasPrefix(e.focus, "/org/freedesktop/IBus/InputContext_") &&
		client != "" && !strings.HasPrefix(client, "fake") &&
		e.content.Purpose != 8 && e.content.Purpose != 9 // PASSWORD, PIN
}

// queuePropertyReply runs under e.mu on the reader. godbus normally dispatches
// getters concurrently, which can reverse FocusId/ActiveSurroundingText replies.
// IBus 1.5 uses that reply order when it upgrades legacy FocusIn to FocusInId.
// A single writer preserves request order without blocking the bus reader.
func (e *insertPureEngine) queuePropertyReply(msg *dbus.Message, member string) bool {
	if msg.Flags&dbus.FlagNoReplyExpected != 0 || e.propertyReplies == nil {
		return false
	}
	properties := map[string]dbus.Variant{"FocusId": dbus.MakeVariant(true), "ActiveSurroundingText": dbus.MakeVariant(false)}
	var value any
	switch {
	case member == "GetAll" && len(msg.Body) == 1 && msg.Body[0] == ibusEngineInterface:
		value = properties
	case member == "Get" && len(msg.Body) == 2 && msg.Body[0] == ibusEngineInterface:
		name, ok := msg.Body[1].(string)
		if !ok {
			return false
		}
		property, ok := properties[name]
		if !ok {
			return false
		}
		value = property
	default:
		return false
	}
	reply := &dbus.Message{Type: dbus.TypeMethodReply, Headers: map[dbus.HeaderField]dbus.Variant{
		dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
		dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf(value)),
	}, Body: []any{value}}
	if sender, ok := msg.Headers[dbus.FieldSender]; ok {
		reply.Headers[dbus.FieldDestination] = sender
	}
	select {
	case e.propertyReplies <- reply:
		// The exported getter still validates the call, but godbus must not
		// also generate a concurrent automatic reply for this queued response.
		msg.Flags |= dbus.FlagNoReplyExpected
		return true
	default:
		e.destroyed = true // An unexpected request flood must fail closed.
		return false
	}
}

func (e *insertPureEngine) export(c *ibusConnection) error {
	e.mu.Lock()
	e.propertyReplies = make(chan *dbus.Message, 16)
	replies := e.propertyReplies
	e.mu.Unlock()
	go func() {
		for {
			select {
			case <-c.Context().Done():
				return
			case reply := <-replies:
				if call := c.Send(reply, nil); call.Err != nil {
					e.mu.Lock()
					e.destroyed = true
					e.mu.Unlock()
					return
				}
			}
		}
	}()
	noop := func(s dbus.Sender) *dbus.Error { return e.authorize(s) }
	methods := map[string]any{}
	for _, name := range []string{"Enable", "Disable", "FocusIn", "FocusOut", "Reset", "PageUp", "PageDown", "CursorUp", "CursorDown"} {
		methods[name] = noop
	}
	methods["FocusInId"] = func(s dbus.Sender, _, _ string) *dbus.Error { return e.authorize(s) }
	methods["FocusOutId"] = func(s dbus.Sender, _ string) *dbus.Error { return e.authorize(s) }
	methods["ProcessKeyEvent"] = func(s dbus.Sender, _, _, _ uint32) (bool, *dbus.Error) { return false, e.authorize(s) }
	methods["SetCursorLocation"] = func(s dbus.Sender, _, _, _, _ int32) *dbus.Error { return e.authorize(s) }
	methods["SetCapabilities"] = func(s dbus.Sender, _ uint32) *dbus.Error { return e.authorize(s) }
	methods["SetSurroundingText"] = func(s dbus.Sender, _ dbus.Variant, _, _ uint32) *dbus.Error { return e.authorize(s) }
	methods["PropertyActivate"] = func(s dbus.Sender, _ string, _ uint32) *dbus.Error { return e.authorize(s) }
	for _, name := range []string{"PropertyShow", "PropertyHide"} {
		methods[name] = func(s dbus.Sender, _ string) *dbus.Error { return e.authorize(s) }
	}
	methods["CandidateClicked"] = func(s dbus.Sender, _, _, _ uint32) *dbus.Error { return e.authorize(s) }
	methods["ProcessHandWritingEvent"] = func(s dbus.Sender, _ []float64) *dbus.Error { return e.authorize(s) }
	methods["CancelHandWriting"] = func(s dbus.Sender, _ uint32) *dbus.Error { return e.authorize(s) }
	for _, name := range []string{"PanelExtensionReceived", "PanelExtensionRegisterKeys"} {
		methods[name] = func(s dbus.Sender, _ dbus.Variant) *dbus.Error { return e.authorize(s) }
	}
	if err := c.ExportMethodTable(methods, ibusEnginePath, ibusEngineInterface); err != nil {
		return err
	}
	properties := map[string]any{
		"Get": func(s dbus.Sender, iface, name string) (dbus.Variant, *dbus.Error) {
			if err := e.authorize(s); err != nil {
				return dbus.Variant{}, err
			}
			if iface == ibusEngineInterface {
				switch name {
				case "FocusId":
					return dbus.MakeVariant(true), nil
				case "ActiveSurroundingText":
					return dbus.MakeVariant(false), nil
				}
			}
			return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []any{"unknown engine property"})
		},
		"GetAll": func(s dbus.Sender, iface string) (map[string]dbus.Variant, *dbus.Error) {
			if err := e.authorize(s); err != nil {
				return nil, err
			}
			if iface != ibusEngineInterface {
				err := dbus.MakeUnknownInterfaceError(iface)
				return nil, &err
			}
			return map[string]dbus.Variant{"FocusId": dbus.MakeVariant(true), "ActiveSurroundingText": dbus.MakeVariant(false)}, nil
		},
		"Set": func(s dbus.Sender, iface, name string, value dbus.Variant) *dbus.Error {
			if err := e.authorize(s); err != nil {
				return err
			}
			var content ibusContentType
			if iface != ibusEngineInterface || name != "ContentType" || value.Signature().String() != "(uu)" || dbus.Store([]any{value.Value()}, &content) != nil {
				return &dbus.ErrMsgInvalidArg
			}
			return nil // observe already applied this update in receive order.
		},
	}
	if err := c.ExportMethodTable(properties, ibusEnginePath, ibusProperties); err != nil {
		return err
	}
	for _, path := range []dbus.ObjectPath{ibusEnginePath, ibusFactoryPath} {
		if err := c.ExportMethodTable(map[string]any{"Destroy": noop}, path, ibusService+".Service"); err != nil {
			return err
		}
	}
	return c.ExportMethodTable(map[string]any{"CreateEngine": func(s dbus.Sender, name string) (dbus.ObjectPath, *dbus.Error) {
		if err := e.authorize(s); err != nil {
			return "/", err
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		if name != e.name || e.created || e.destroyed || time.Now().After(e.deadline) {
			return "/", dbus.NewError(ibusService+".Error.Failed", []any{"temporary engine is unavailable"})
		}
		e.created = true
		return ibusEnginePath, nil
	}}, ibusFactoryPath, ibusService+".Factory")
}

type ibusTextInserter struct {
	mu                     sync.Mutex
	connection             *ibusConnection
	engine                 *insertPureEngine
	address, previous      string
	registered, activation bool
	closed                 bool
}

func init() {
	textInserterBackends[InsertBackendIBus] = func(ctx context.Context) (TextInserter, error) {
		inserter, err := prepareIBusInserter(ctx, time.Now().Add(ibusSetupLimit))
		if err != nil {
			return nil, err
		}
		return inserter, nil
	}
}

// Preparation activates the engine before the caller's focus window. IBus can
// initially send legacy FocusIn while its asynchronous FocusId query is pending.
// Submission still requires a later FocusInId with a real client identity.
func prepareIBusInserter(ctx context.Context, deadline time.Time) (_ *ibusTextInserter, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	setup, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	address, err := ibusAddress(os.Getenv, readIBusMetadata, func(pid int) bool { return syscall.Kill(pid, 0) == nil })
	if err != nil {
		return nil, err
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, errors.New("cannot create a temporary engine name")
	}
	engine := &insertPureEngine{name: "insert-pure-" + hex.EncodeToString(nonce[:]), deadline: deadline, changed: make(chan struct{}, 1)}
	c, err := connectIBus(setup, address, engine.observe)
	if err != nil {
		return nil, err
	}
	_ = c.socket.SetWriteDeadline(deadline)
	inserter := &ibusTextInserter{connection: c, engine: engine, address: address}
	defer func() {
		if err != nil {
			err = errors.Join(err, inserter.Close())
		}
	}()
	var owner string
	if err = c.BusObject().CallWithContext(setup, "org.freedesktop.DBus.GetNameOwner", 0, ibusService).Store(&owner); err != nil {
		return nil, errors.New("cannot identify the IBus service")
	}
	engine.mu.Lock()
	engine.owner = owner
	engine.mu.Unlock()
	inserter.previous, err = c.globalEngine(setup)
	if err != nil {
		return nil, err
	}
	if err = engine.export(c); err != nil {
		return nil, errors.New("cannot export the temporary IBus factory")
	}
	inserter.registered = true // A timed-out registration might still have taken effect.
	if err = c.Object(ibusService, ibusPath).CallWithContext(setup, ibusService+".RegisterComponent", 0, insertPureComponent(engine.name)).Store(); err != nil {
		return nil, errors.New("cannot register the temporary IBus component before the deadline")
	}
	inserter.activation = true
	if err = c.Object(ibusService, ibusPath).CallWithContext(setup, ibusService+".SetGlobalEngine", 0, engine.name).Store(); err != nil {
		return nil, errors.New("cannot activate the temporary IBus engine before the deadline")
	}
	if !time.Now().Before(deadline) {
		return nil, errors.New("IBus activation missed the setup deadline")
	}
	// The caller can wait before insertion. Do not leave the setup deadline on
	// the socket: engine property replies must still work during that interval.
	_ = c.socket.SetWriteDeadline(time.Time{})
	return inserter, nil
}

func (i *ibusTextInserter) Insert(ctx context.Context, text string) error {
	return i.insertAt(ctx, text, time.Time{})
}

func (i *ibusTextInserter) insertAt(ctx context.Context, text string, deadline time.Time) (err error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return ErrTextInserterClosed
	}
	defer func() { err = errors.Join(err, i.cleanup()) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateInsertionText(text); err != nil {
		return err
	}
	if i.connection == nil {
		prepared, err := prepareIBusInserter(ctx, time.Now().Add(ibusSetupLimit))
		if err != nil {
			return err
		}
		i.connection, i.engine = prepared.connection, prepared.engine
		i.address, i.previous = prepared.address, prepared.previous
		i.registered, i.activation = prepared.registered, prepared.activation
	}
	if deadline.IsZero() {
		if err := i.waitReady(ctx); err != nil {
			return err
		}
		deadline = time.Now()
	}
	i.engine.mu.Lock()
	i.engine.deadline = deadline
	i.engine.mu.Unlock()
	_ = i.connection.socket.SetWriteDeadline(deadline.Add(insertPureSendGrace))
	return submitInsertPure(ctx, i.connection, i.engine, text)
}

// waitReady waits only for outstanding IBus callbacks, not a fixed focus delay.
func (i *ibusTextInserter) waitReady(ctx context.Context) error {
	ready, cancel := context.WithTimeout(ctx, ibusSetupLimit)
	defer cancel()
	deadline, _ := ready.Deadline()
	for {
		i.engine.mu.Lock()
		i.engine.deadline = deadline
		eligible, destroyed := i.engine.eligible(), i.engine.destroyed
		protected := i.engine.content.Purpose == 8 || i.engine.content.Purpose == 9
		fake := strings.HasPrefix(strings.ToLower(i.engine.client), "fake")
		i.engine.mu.Unlock()
		if protected || fake {
			return errors.New("IBus focus is a password, PIN, or fake input context; no text submitted")
		}
		if eligible {
			return nil
		}
		if destroyed {
			return errors.New("the temporary IBus engine was destroyed before it became ready")
		}
		select {
		case <-ready.Done():
			return fmt.Errorf("IBus did not provide an eligible focused input context: %w", ready.Err())
		case <-i.engine.changed:
		}
	}
}

func (i *ibusTextInserter) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.close()
}

// close requires i.mu. Cleanup is attempted once, including after an error.
func (i *ibusTextInserter) close() error {
	if i.closed {
		return nil
	}
	i.closed = true
	return i.cleanup()
}

// cleanup releases the current temporary engine, not the reusable inserter.
// It requires i.mu. A failed cleanup closes the inserter to prevent reuse while
// the desktop engine's ownership is uncertain.
func (i *ibusTextInserter) cleanup() error {
	if i.connection == nil {
		return nil
	}
	err := cleanupInsertPure(i.connection, i.address, i.engine.name, i.previous, i.registered, i.activation)
	i.connection, i.engine = nil, nil
	if err != nil {
		i.closed = true
	}
	return err
}

func submitInsertPure(ctx context.Context, c *ibusConnection, engine *insertPureEngine, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if time.Now().After(engine.deadline.Add(insertPureSendGrace)) {
		return errors.New("the IBus submission deadline expired")
	}
	engine.mu.Lock()
	ready, focus := engine.eligible(), engine.focus
	engine.mu.Unlock()
	if !ready {
		return errors.New("no enabled, eligible IBus input context at insertion time")
	}
	send, cancel := context.WithDeadline(ctx, engine.deadline.Add(insertPureSendGrace))
	defer cancel()
	current, err := c.globalEngine(send)
	if err != nil || current != engine.name {
		return errors.New("the temporary IBus engine is no longer selected")
	}
	value, err := c.property(send, "CurrentInputContext")
	if err != nil || ibusUnwrap(value) != dbus.ObjectPath(focus) {
		return errors.New("IBus focus changed before submission")
	}
	engine.mu.Lock()
	if !engine.eligible() || engine.focus != focus || send.Err() != nil || time.Now().After(engine.deadline.Add(insertPureSendGrace)) {
		engine.mu.Unlock()
		return errors.New("IBus readiness changed or the submission deadline expired")
	}
	engine.submitted = true // Never retry an unacknowledged signal.
	err = c.Emit(ibusEnginePath, ibusEngineInterface+".CommitText", insertPureWireText(text))
	engine.mu.Unlock()
	if err != nil {
		return errors.New("IBus CommitText submission failed; it will not be retried")
	}
	// A round trip on the same connection keeps teardown after the signal.
	// This is a daemon barrier, NOT acknowledgement from the application.
	if _, err = c.property(send, "CurrentInputContext"); err != nil {
		return errors.New("IBus CommitText was sent, but the daemon barrier failed; it will not be retried")
	}
	return nil
}

func cleanupInsertPure(c *ibusConnection, address, temporary, previous string, registered, activation bool) error {
	if !registered {
		c.close()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), insertPureCleanupLimit)
	defer cancel()
	deadline, _ := ctx.Deadline()
	_ = c.socket.SetWriteDeadline(deadline)
	current, beforeErr := c.globalEngine(ctx)
	restore := activation && (current == temporary || errors.Is(beforeErr, errIBusNoGlobalEngine))
	oldName := c.Names()[0]
	c.close() // Teardown MUST precede restoration, including on cancellation.
	control, err := connectIBus(ctx, address, nil)
	if err != nil {
		return errors.New("cannot reconnect to IBus for cleanup; check the selected engine")
	}
	defer control.close()
	_ = control.socket.SetWriteDeadline(deadline)
	if err := waitInsertPureRemoved(ctx, control, oldName, temporary); err != nil {
		return err
	}
	if beforeErr != nil && !errors.Is(beforeErr, errIBusNoGlobalEngine) {
		return errors.New("cannot determine engine ownership during cleanup; restoration was skipped")
	}
	if !restore {
		return nil
	} // Preserve an unrelated user selection.
	current, err = control.globalEngine(ctx)
	if err != nil && !errors.Is(err, errIBusNoGlobalEngine) {
		return errors.New("cannot check the engine after teardown; restoration was skipped")
	}
	if current == previous {
		return nil
	}
	if err == nil && current != temporary {
		return nil
	}
	// IBus has no compare-and-swap API. The two ownership checks reduce, but
	// cannot eliminate, a concurrent user-selection race.
	if err = control.Object(ibusService, ibusPath).CallWithContext(ctx, ibusService+".SetGlobalEngine", 0, previous).Store(); err != nil {
		return errors.New("cannot restore the previous IBus engine")
	}
	current, err = control.globalEngine(ctx)
	if err != nil || current != previous {
		return errors.New("IBus engine restoration could not be verified")
	}
	return nil
}

func waitInsertPureRemoved(ctx context.Context, c *ibusConnection, oldName, temporary string) error {
	for {
		var present bool
		if err := c.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, oldName).Store(&present); err != nil {
			return errors.New("cannot verify temporary IBus connection removal")
		}
		value, err := c.property(ctx, "ActiveEngines")
		if err != nil {
			return errors.New("cannot verify temporary IBus component removal")
		}
		var engines []dbus.Variant
		if ibusStore(value, &engines) != nil {
			return errors.New("invalid IBus active-engine list during cleanup")
		}
		found := false
		for _, engine := range engines {
			name, err := ibusEngineName(engine)
			if err != nil {
				return err
			}
			found = found || name == temporary
		}
		if !present && !found {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("temporary IBus component cleanup: %w", ctx.Err())
		case <-timer.C:
		}
	}
}
