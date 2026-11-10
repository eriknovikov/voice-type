package voicetype

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

const testIBusFocus = "/org/freedesktop/IBus/InputContext_42"

func testInsertEngine() *insertPureEngine {
	return &insertPureEngine{
		name: "insert-pure-test", owner: ":1.9", created: true, enabled: true, contentKnown: true,
		deadline: time.Now().Add(time.Second), changed: make(chan struct{}, 1),
	}
}

func testEngineMessage(iface, member string, body ...any) *dbus.Message {
	return &dbus.Message{Type: dbus.TypeMethodCall, Headers: map[dbus.HeaderField]dbus.Variant{
		dbus.FieldPath: dbus.MakeVariant(ibusEnginePath), dbus.FieldSender: dbus.MakeVariant("org.freedesktop.DBus"),
		dbus.FieldInterface: dbus.MakeVariant(iface), dbus.FieldMember: dbus.MakeVariant(member),
	}, Body: body}
}

func testEngineObserve(e *insertPureEngine, member string, body ...any) {
	e.observe(testEngineMessage(ibusEngineInterface, member, body...))
}

func TestIBusFocusProtocols(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("Legacy%v", legacy), func(t *testing.T) {
			e := testInsertEngine()
			if legacy {
				testEngineObserve(e, "FocusIn")
				if e.eligible() {
					t.Fatal("unbound legacy focus became eligible")
				}
				e.bindFocus(e.focusGeneration, testIBusFocus)
			} else {
				testEngineObserve(e, "FocusInId", testIBusFocus, "gtk-im")
			}
			if !e.eligible() {
				t.Fatal("valid focus is not eligible")
			}
			generation := e.focusGeneration
			// Newer daemons replay this after negotiating FocusId. Replays
			// must preserve cached content, Enable, and the same generation.
			for range 2 {
				testEngineObserve(e, "FocusInId", testIBusFocus, "gtk-im")
				if !e.eligible() || e.focusGeneration != generation {
					t.Fatal("ID upgrade or duplicate invalidated the same focus")
				}
			}
			testEngineObserve(e, "FocusOutId", "/org/freedesktop/IBus/InputContext_99")
			if !e.eligible() {
				t.Fatal("unrelated FocusOutId cleared focus")
			}
			testEngineObserve(e, "FocusOutId", testIBusFocus)
			if e.eligible() || e.focused || e.focusGeneration == generation {
				t.Fatal("FocusOutId did not invalidate focus")
			}
		})
	}
}

func TestIBusStaleLegacyBinding(t *testing.T) {
	for _, event := range []string{"FocusIn", "FocusOut", "FocusOutId", "Disable", "Destroy", "OtherContext"} {
		t.Run(event, func(t *testing.T) {
			e := testInsertEngine()
			testEngineObserve(e, "FocusIn")
			generation := e.focusGeneration
			switch event {
			case "FocusOutId":
				testEngineObserve(e, event, testIBusFocus) // Can arrive before any ID was bound.
			case "Destroy":
				e.observe(testEngineMessage(ibusService+".Service", event))
			case "OtherContext":
				testEngineObserve(e, "FocusInId", "/org/freedesktop/IBus/InputContext_99", "other")
			default:
				testEngineObserve(e, event)
			}
			e.bindFocus(generation, testIBusFocus)
			if e.focus == testIBusFocus || e.focusGeneration == generation {
				t.Fatal("stale context read was accepted")
			}
		})
	}
}

func TestIBusEligibility(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*insertPureEngine)
	}{
		{"not created", func(e *insertPureEngine) { e.created = false }},
		{"destroyed", func(e *insertPureEngine) { e.destroyed = true }},
		{"disabled", func(e *insertPureEngine) { e.enabled = false }},
		{"not focused", func(e *insertPureEngine) { e.focused = false }},
		{"unknown content", func(e *insertPureEngine) { e.contentKnown = false }},
		{"submitted", func(e *insertPureEngine) { e.submitted = true }},
		{"late focus", func(e *insertPureEngine) { e.focusAt = e.deadline.Add(time.Nanosecond) }},
		{"late enable", func(e *insertPureEngine) { e.enabledAt = e.deadline.Add(time.Nanosecond) }},
		{"password", func(e *insertPureEngine) { e.content.Purpose = 8 }},
		{"pin", func(e *insertPureEngine) { e.content.Purpose = 9 }},
		{"fake", func(e *insertPureEngine) { e.client = "FaKe-client" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := testInsertEngine()
			testEngineObserve(e, "FocusInId", testIBusFocus, "client")
			tc.change(e)
			if e.eligible() {
				t.Fatal("unsafe state is eligible")
			}
		})
	}
	for _, path := range []string{"", "/", "/org/freedesktop/IBus/InputContext_", testIBusFocus + "/child", "/other_42", testIBusFocus + "-bad"} {
		t.Run(path, func(t *testing.T) {
			e := testInsertEngine()
			testEngineObserve(e, "FocusIn")
			e.bindFocus(e.focusGeneration, path)
			if e.eligible() || e.focus != "" {
				t.Fatalf("invalid context accepted: %q", path)
			}
		})
	}
}

func TestIBusUntrustedAndMalformedCallbacks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*dbus.Message)
	}{
		{"sender", func(m *dbus.Message) { m.Headers[dbus.FieldSender] = dbus.MakeVariant(":1.666") }},
		{"path", func(m *dbus.Message) { m.Headers[dbus.FieldPath] = dbus.MakeVariant(dbus.ObjectPath("/other")) }},
		{"interface", func(m *dbus.Message) { m.Headers[dbus.FieldInterface] = dbus.MakeVariant("other.Interface") }},
		{"type", func(m *dbus.Message) { m.Type = dbus.TypeSignal }},
		{"arity", func(m *dbus.Message) { m.Body = m.Body[:1] }},
		{"argument type", func(m *dbus.Message) { m.Body[0] = uint32(42) }},
		{"invalid path", func(m *dbus.Message) { m.Body[0] = "/other" }},
		{"empty client", func(m *dbus.Message) { m.Body[1] = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := testInsertEngine()
			msg := testEngineMessage(ibusEngineInterface, "FocusInId", testIBusFocus, "client")
			tc.change(msg)
			e.observe(msg)
			if e.focused || e.eligible() {
				t.Fatal("invalid callback established focus")
			}
		})
	}
	e := testInsertEngine()
	msg := testEngineMessage(ibusEngineInterface, "FocusInId", testIBusFocus, "client")
	msg.Headers[dbus.FieldSender] = dbus.MakeVariant(e.owner)
	e.observe(msg)
	if !e.eligible() {
		t.Fatal("authenticated service owner was rejected")
	}
	if e.authorize(dbus.Sender(":1.666")) == nil {
		t.Fatal("untrusted exported method was authorized")
	}
}

func TestIBusContentAndEnableCaching(t *testing.T) {
	e := testInsertEngine()
	e.enabled, e.contentKnown = false, false
	testEngineObserve(e, "FocusIn")
	e.bindFocus(e.focusGeneration, testIBusFocus) // Disabled engines cannot bind.
	if e.focus != "" {
		t.Fatal("disabled engine accepted binding")
	}
	testEngineObserve(e, "Enable")
	e.bindFocus(e.focusGeneration, testIBusFocus)
	if e.eligible() {
		t.Fatal("initial ContentType was not required")
	}
	content := func(value any) {
		e.observe(testEngineMessage(ibusProperties, "Set", ibusEngineInterface, "ContentType", dbus.MakeVariant(value)))
	}
	content("not a content type")
	if e.contentKnown {
		t.Fatal("malformed ContentType was accepted")
	}
	content(ibusContentType{Purpose: 8})
	if !e.contentKnown || e.eligible() {
		t.Fatal("password ContentType was not applied")
	}
	content(ibusContentType{})
	testEngineObserve(e, "FocusOut")
	testEngineObserve(e, "FocusInId", testIBusFocus, "client")
	if !e.eligible() {
		t.Fatal("unchanged cached ContentType and Enable were lost")
	}
	testEngineObserve(e, "Disable")
	if e.eligible() {
		t.Fatal("disabled engine remained eligible")
	}
	testEngineObserve(e, "Enable")
	if !e.eligible() {
		t.Fatal("re-enabled, focused engine did not become eligible")
	}
}

func TestIBusWaitReady(t *testing.T) {
	for _, tc := range []string{"legacy", "modern", "password update", "normal update", "fake upgrade", "stale lookup", "malformed context", "unknown content", "canceled", "destroyed", "no focus", "disconnect"} {
		t.Run(tc, func(t *testing.T) {
			e := testInsertEngine()
			testEngineObserve(e, "FocusIn")
			if tc == "modern" {
				testEngineObserve(e, "FocusInId", testIBusFocus, "client")
			}
			if tc == "normal update" {
				e.content.Purpose = 8
			}
			if tc == "unknown content" {
				e.contentKnown = false
			}
			c := testIBusPeer(t, func(msg *dbus.Message) ([]any, error) {
				switch tc {
				case "password update":
					e.observe(testEngineMessage(ibusProperties, "Set", ibusEngineInterface, "ContentType", dbus.MakeVariant(ibusContentType{Purpose: 8})))
				case "normal update":
					e.observe(testEngineMessage(ibusProperties, "Set", ibusEngineInterface, "ContentType", dbus.MakeVariant(ibusContentType{})))
				case "malformed context":
					return []any{dbus.MakeVariant("not an object path")}, nil
				case "fake upgrade":
					testEngineObserve(e, "FocusInId", testIBusFocus, "fake-client")
				case "stale lookup":
					testEngineObserve(e, "FocusOut")
				case "disconnect":
					return nil, io.EOF
				}
				return []any{dbus.MakeVariant(dbus.ObjectPath(testIBusFocus))}, nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			switch tc {
			case "canceled":
				cancel()
			case "destroyed":
				e.observe(testEngineMessage(ibusService+".Service", "Destroy"))
			case "no focus":
				testEngineObserve(e, "FocusOut")
			}
			err := (&ibusTextInserter{engine: e, connection: c}).waitReady(ctx)
			wantSuccess := tc == "legacy" || tc == "modern" || tc == "normal update"
			if (err == nil) != wantSuccess {
				t.Fatalf("waitReady: %v; want success %v", err, wantSuccess)
			}
			if tc == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}

func TestIBusSubmission(t *testing.T) {
	for _, tc := range []string{"success", "canceled", "canceled during validation", "expired", "wrong engine", "wrong context", "same path refocused", "disabled", "destroyed", "password", "barrier failure", "disconnect"} {
		t.Run(tc, func(t *testing.T) {
			e := testInsertEngine()
			testEngineObserve(e, "FocusInId", testIBusFocus, "client")
			var commits atomic.Int32
			var reads int
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := testIBusPeer(t, func(msg *dbus.Message) ([]any, error) {
				if msg.Type == dbus.TypeSignal {
					commits.Add(1)
					return nil, nil
				}
				if msg.Body[1] == "GlobalEngine" {
					name := e.name
					if tc == "wrong engine" {
						name = "other"
					}
					return []any{dbus.MakeVariant(ibusEngineDesc{Type: "IBusEngineDesc", Name: name})}, nil
				}
				reads++
				if reads == 2 && tc == "barrier failure" || tc == "disconnect" {
					return nil, io.EOF
				}
				switch tc {
				case "canceled during validation":
					cancel()
				case "same path refocused":
					testEngineObserve(e, "FocusOut")
					testEngineObserve(e, "FocusInId", testIBusFocus, "client")
				case "disabled":
					testEngineObserve(e, "Disable")
				case "destroyed":
					e.observe(testEngineMessage(ibusService+".Service", "Destroy"))
				case "password":
					e.observe(testEngineMessage(ibusProperties, "Set", ibusEngineInterface, "ContentType", dbus.MakeVariant(ibusContentType{Purpose: 9})))
				case "wrong context":
					return []any{dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/IBus/InputContext_99"))}, nil
				}
				return []any{dbus.MakeVariant(dbus.ObjectPath(testIBusFocus))}, nil
			})
			if tc == "canceled" {
				cancel()
			}
			if tc == "expired" {
				e.deadline = time.Now().Add(-time.Second)
			}
			notified := false
			err := submitInsertPureWithSubmission(ctx, c, e, "test text", func() {
				if reads != 1 {
					t.Error("submission timing occurred after daemon barrier")
				}
				notified = true
			})
			if notified != (tc == "success" || tc == "barrier failure") {
				t.Fatal("submission notification did not match actual send")
			}
			if (err == nil) != (tc == "success") {
				t.Fatalf("submit: %v", err)
			}
			wantCommits := int32(0)
			if tc == "success" || tc == "barrier failure" {
				wantCommits = 1
				if !e.submitted {
					t.Fatal("emission did not mark submitted")
				}
				if err := submitInsertPure(context.Background(), c, e, "retry"); err == nil {
					t.Fatal("second submission was allowed")
				}
			}
			if got := commits.Load(); got != wantCommits {
				t.Fatalf("commits = %d; want %d", got, wantCommits)
			}
		})
	}
}

func TestIBusInsertionValidation(t *testing.T) {
	for _, text := range []string{"", "\xff", "a\x00b"} {
		inserter := &ibusTextInserter{}
		if err := inserter.Insert(context.Background(), text); err == nil {
			t.Fatalf("invalid text accepted: %q", text)
		}
		if inserter.connection != nil {
			t.Fatal("invalid input connected to IBus")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewTextInserter(ctx, InsertBackendIBus); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled constructor: %v", err)
	}
}

// A minimal authenticated D-Bus peer over net.Pipe keeps transport failure and
// receive-order tests hermetic: no installed daemon, sockets, or subprocesses.
// The real-daemon suite separately checks actual routing and application receipt.
func testIBusPeer(t *testing.T, handle func(*dbus.Message) ([]any, error)) *ibusConnection {
	t.Helper()
	client, server := net.Pipe()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	_ = server.SetDeadline(time.Now().Add(3 * time.Second))
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		r := bufio.NewReader(server)
		for _, response := range []string{"REJECTED EXTERNAL\r\n", "OK 0123456789abcdef0123456789abcdef\r\n", ""} {
			if _, err := r.ReadString('\n'); err != nil {
				return
			}
			if response != "" {
				if _, err := io.WriteString(server, response); err != nil {
					return
				}
			}
		}
		for serial := uint32(1); ; serial++ {
			msg, err := dbus.DecodeMessage(r)
			if err != nil {
				return
			}
			var body []any
			if msg.Headers[dbus.FieldMember].Value() == "Hello" {
				body = []any{":1.42"}
			} else {
				body, err = handle(msg)
			}
			if err != nil {
				return
			}
			if msg.Type != dbus.TypeMethodCall {
				continue
			}
			reply := &dbus.Message{Type: dbus.TypeMethodReply, Headers: map[dbus.HeaderField]dbus.Variant{
				dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
				dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf(body...)),
			}, Body: body}
			var encoded bytes.Buffer
			if reply.EncodeTo(&encoded, binary.LittleEndian) != nil {
				return
			}
			binary.LittleEndian.PutUint32(encoded.Bytes()[8:12], serial)
			if _, err := server.Write(encoded.Bytes()); err != nil {
				return
			}
		}
	}()
	conn, err := dbus.NewConn(client)
	if err == nil {
		err = conn.Auth([]dbus.Auth{dbus.AuthExternal("1000")})
	}
	if err == nil {
		err = conn.Hello()
	}
	if err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	c := &ibusConnection{Conn: conn, socket: client}
	t.Cleanup(func() {
		c.close()
		<-done
	})
	return c
}

func TestIBusEngineDescriptionVersions(t *testing.T) {
	// IBus appends optional fields to this tuple; 1.5.10 lacks IconPropKey.
	// Only the stable serializable header and engine name are needed here.
	prefix := []any{"IBusEngineDesc", map[string]dbus.Variant{}, "engine"}
	for _, fields := range [][]any{prefix, append(append([]any{}, prefix...), "future", uint32(1))} {
		name, err := ibusEngineName(dbus.MakeVariant(fields))
		if err != nil || name != "engine" {
			t.Fatalf("engine prefix: %q, %v", name, err)
		}
	}
	for _, fields := range [][]any{nil, prefix[:2], {"WrongType", map[string]dbus.Variant{}, "engine"}, {"IBusEngineDesc", "wrong attachments", "engine"}, {"IBusEngineDesc", map[string]dbus.Variant{}, ""}, {"IBusEngineDesc", map[string]dbus.Variant{}, uint32(1)}} {
		if _, err := ibusEngineName(dbus.MakeVariant(fields)); err == nil {
			t.Fatalf("invalid description accepted: %#v", fields)
		}
	}
}

func TestIBusPropertyReplyOrder(t *testing.T) {
	e := testInsertEngine()
	e.propertyReplies = make(chan *dbus.Message, 2)
	for _, name := range []string{"FocusId", "ActiveSurroundingText"} {
		msg := testEngineMessage(ibusProperties, "Get", ibusEngineInterface, name)
		e.observe(msg)
		if msg.Flags&dbus.FlagNoReplyExpected == 0 {
			t.Fatal("queued reply was not suppressed in the exported handler")
		}
	}
	for _, want := range []bool{true, false} {
		reply := <-e.propertyReplies
		if value, ok := reply.Body[0].(dbus.Variant); !ok || value.Value() != want {
			t.Fatalf("wrong property reply order: %#v", reply.Body)
		}
	}
	for range 3 {
		e.observe(testEngineMessage(ibusProperties, "Get", ibusEngineInterface, "FocusId"))
	}
	if !e.destroyed {
		t.Fatal("property reply flood did not fail closed")
	}
}

func TestIBusTruncatedMessage(t *testing.T) {
	// connectIBus uses godbus's generic stream transport. A disconnect in
	// header padding used to panic its reader goroutine in godbus <= 5.2.2.
	msg := &dbus.Message{Type: dbus.TypeSignal, Headers: map[dbus.HeaderField]dbus.Variant{
		dbus.FieldPath:      dbus.MakeVariant(dbus.ObjectPath("/a")),
		dbus.FieldInterface: dbus.MakeVariant("test.I"), dbus.FieldMember: dbus.MakeVariant("X"),
	}}
	var frame bytes.Buffer
	if err := msg.EncodeTo(&frame, binary.LittleEndian); err != nil {
		t.Fatal(err)
	}
	headerEnd := 16 + int(binary.LittleEndian.Uint32(frame.Bytes()[12:16]))
	if headerEnd%8 == 0 {
		t.Fatal("fixture must have header padding")
	}
	for _, length := range []int{0, 1, 10, headerEnd, frame.Len() - 1} {
		if _, err := dbus.DecodeMessage(bytes.NewReader(frame.Bytes()[:length])); err == nil {
			t.Fatalf("truncated frame of %d bytes accepted", length)
		}
	}
}

func TestIBusWireText(t *testing.T) {
	text := "日本語\n🦊"
	var decoded ibusText
	if err := ibusStore(insertPureWireText(text), &decoded); err != nil || decoded.Text != text || decoded.Type != "IBusText" {
		t.Fatalf("wire text: %+v, %v", decoded, err)
	}
	if sig := insertPureWireText(text).Signature().String(); sig != "(sa{sv}sv)" {
		t.Fatalf("invalid IBus text signature: %s", sig)
	}
}
