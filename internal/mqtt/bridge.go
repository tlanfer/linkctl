package mqtt

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"

	"github.com/tlanfer/linkctl/internal/camera"
	"github.com/tlanfer/linkctl/internal/config"
	"github.com/tlanfer/linkctl/internal/service"
)

// Connection states reported by Status.
const (
	StateDisabled   = "disabled"
	StateConnecting = "connecting"
	StateConnected  = "connected"
	StateError      = "error"
)

// Status describes the broker connection.
type Status struct {
	State    string `json:"state"`
	Error    string `json:"error,omitempty"`
	Broker   string `json:"broker,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
	Topic    string `json:"topic,omitempty"`
}

// Bridge maintains the broker connection according to the config.
type Bridge struct {
	svc     *service.Service
	store   *config.Store
	secrets config.Secrets
	log     *slog.Logger

	reload chan struct{}

	mu     sync.Mutex
	status Status
	subs   map[chan struct{}]struct{}
}

// NewBridge creates a bridge; call Run to start it.
func NewBridge(svc *service.Service, store *config.Store, secrets config.Secrets, log *slog.Logger) *Bridge {
	if log == nil {
		log = slog.Default()
	}
	return &Bridge{
		svc: svc, store: store, secrets: secrets, log: log,
		reload: make(chan struct{}, 1),
		status: Status{State: StateDisabled},
		subs:   map[chan struct{}]struct{}{},
	}
}

// Status returns the current connection status.
func (b *Bridge) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status
}

// Subscribe signals status changes.
func (b *Bridge) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}

func (b *Bridge) setStatus(s Status) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s == b.status {
		return
	}
	b.status = s
	for ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Reload reconnects with the current config (call after settings change).
func (b *Bridge) Reload() {
	select {
	case b.reload <- struct{}{}:
	default:
	}
}

type session struct {
	cfg      config.MQTT
	password string
	deviceID string
}

// Run manages the connection until ctx is cancelled.
func (b *Bridge) Run(ctx context.Context) {
	changes, cancel := b.svc.Subscribe()
	defer cancel()
	for {
		cfg := b.store.Get().MQTT
		sess := session{cfg: cfg, password: config.LoadPassword(b.store.Get(), b.secrets), deviceID: b.svc.Snapshot().DeviceID}
		if !cfg.Enabled || cfg.Host == "" {
			st := Status{State: StateDisabled}
			if cfg.Enabled {
				st.Error = "no broker host configured"
			}
			b.setStatus(st)
			select {
			case <-ctx.Done():
				return
			case <-b.reload:
				continue
			}
		}
		sctx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		restart := make(chan struct{}, 1)
		go func() {
			defer close(done)
			b.runSession(sctx, sess, changes, restart)
		}()
		select {
		case <-ctx.Done():
			stop()
			<-done
			return
		case <-b.reload:
		case <-restart:
		}
		stop()
		<-done
	}
}

func brokerURL(c config.MQTT) (*url.URL, error) {
	scheme := "mqtt"
	if c.TLS != config.TLSOff {
		scheme = "tls"
	}
	host := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	return url.Parse(scheme + "://" + host)
}

func tlsConfig(c config.MQTT) *tls.Config {
	if c.TLS == config.TLSOff {
		return nil
	}
	return &tls.Config{ServerName: c.Host, InsecureSkipVerify: c.TLS == config.TLSInsecure, MinVersion: tls.VersionTLS12}
}

func clientConfig(sess session, topics Topics, u *url.URL) autopaho.ClientConfig {
	return autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{u},
		TlsCfg:                        tlsConfig(sess.cfg),
		KeepAlive:                     30,
		CleanStartOnInitialConnection: true,
		ConnectTimeout:                10 * time.Second,
		ReconnectBackoff:              autopaho.NewExponentialBackoff(time.Second, 30*time.Second, 2*time.Second, 2),
		ConnectUsername:               sess.cfg.Username,
		ConnectPassword:               []byte(sess.password),
		WillMessage: &paho.WillMessage{
			Topic: topics.Availability, Payload: []byte("offline"), QoS: 1, Retain: true,
		},
		ClientConfig: paho.ClientConfig{
			ClientID: "linkctl-" + sess.deviceID,
		},
	}
}

func (b *Bridge) runSession(ctx context.Context, sess session, changes <-chan struct{}, restart chan<- struct{}) {
	topics := TopicsFor(sess.cfg.BaseTopic, sess.cfg.DiscoveryPrefix, sess.deviceID)
	u, err := brokerURL(sess.cfg)
	if err != nil {
		b.setStatus(Status{State: StateError, Error: err.Error()})
		<-ctx.Done()
		return
	}
	base := Status{Broker: u.String(), DeviceID: sess.deviceID, Topic: topics.Base}
	st := base
	st.State = StateConnecting
	b.setStatus(st)

	var pub publisher
	up := make(chan struct{}, 1)
	cc := clientConfig(sess, topics, u)
	cc.OnConnectionUp = func(cm *autopaho.ConnectionManager, _ *paho.Connack) {
		st := base
		st.State = StateConnected
		b.setStatus(st)
		select {
		case up <- struct{}{}:
		default:
		}
	}
	cc.OnConnectError = func(err error) {
		st := base
		st.State = StateError
		st.Error = err.Error()
		b.setStatus(st)
		b.log.Warn("mqtt connect failed", "err", err)
	}
	cc.OnConnectionDown = func() bool {
		st := base
		st.State = StateConnecting
		st.Error = "connection lost"
		b.setStatus(st)
		return true
	}
	cc.OnPublishReceived = []func(paho.PublishReceived) (bool, error){
		func(pr paho.PublishReceived) (bool, error) {
			if pr.Packet.Topic != topics.Set {
				return false, nil
			}
			payload := append([]byte(nil), pr.Packet.Payload...)
			go b.handleCommand(payload)
			return true, nil
		},
	}
	// The connection gets its own context so that on shutdown we can still
	// publish "offline" before tearing it down.
	connCtx, connCancel := context.WithCancel(context.Background())
	defer connCancel()
	cm, err := autopaho.NewConnection(connCtx, cc)
	if err != nil {
		st := base
		st.State = StateError
		st.Error = err.Error()
		b.setStatus(st)
		<-ctx.Done()
		return
	}
	pub.cm = cm
	pub.topics = topics
	pub.deviceName = sess.cfg.DeviceName
	defer func() {
		// Best-effort clean shutdown: mark offline, then disconnect.
		dctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if b.Status().State == StateConnected {
			_, _ = cm.Publish(dctx, &paho.Publish{Topic: topics.Availability, Payload: []byte("offline"), QoS: 1, Retain: true})
		}
		_ = cm.Disconnect(dctx)
		connCancel()
		b.setStatus(Status{State: StateDisabled})
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-up:
			pub.reset()
			if _, err := cm.Subscribe(connCtx, &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: topics.Set, QoS: 1}}}); err != nil {
				b.log.Warn("mqtt subscribe failed", "err", err)
			}
			b.sync(connCtx, &pub)
		case <-changes:
			if id := b.svc.Snapshot().DeviceID; id != sess.deviceID {
				// Camera identity changed (e.g. serial became known): topics change.
				select {
				case restart <- struct{}{}:
				default:
				}
				return
			}
			if b.Status().State == StateConnected {
				b.sync(connCtx, &pub)
			}
		}
	}
}

// publisher remembers what was last published so only changes are sent.
type publisher struct {
	cm         *autopaho.ConnectionManager
	topics     Topics
	deviceName string
	discovery  []byte
	avail      []byte
	state      []byte
}

func (p *publisher) reset() { p.discovery, p.avail, p.state = nil, nil, nil }

func (b *Bridge) sync(ctx context.Context, p *publisher) {
	snap := b.svc.Snapshot()
	pub := func(topic string, last *[]byte, payload []byte) {
		if bytes.Equal(*last, payload) {
			return
		}
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if _, err := p.cm.Publish(pctx, &paho.Publish{Topic: topic, Payload: payload, QoS: 1, Retain: true}); err != nil {
			b.log.Warn("mqtt publish failed", "topic", topic, "err", err)
			return
		}
		*last = payload
	}
	pub(p.topics.Discovery, &p.discovery, DiscoveryPayload(snap, p.topics, p.deviceName))
	if snap.Connected {
		pub(p.topics.State, &p.state, StatePayload(snap))
	}
	avail := []byte("offline")
	if snap.Connected {
		avail = []byte("online")
	}
	pub(p.topics.Availability, &p.avail, avail)
}

func (b *Bridge) handleCommand(payload []byte) {
	snap := b.svc.Snapshot()
	specs := make([]camera.ControlSpec, 0, len(snap.Controls))
	for _, c := range snap.Controls {
		specs = append(specs, c.ControlSpec)
	}
	cmd, err := ParseCommand(payload, specs)
	if err != nil {
		b.log.Warn("mqtt command rejected", "payload", string(payload), "err", err)
	}
	if err := Execute(b.svc, cmd); err != nil {
		b.log.Warn("mqtt command failed", "payload", string(payload), "err", err)
	}
}

// Execute runs a parsed command against the service.
func Execute(svc *service.Service, cmd Command) error {
	var errs []error
	if cmd.Preset != "" && cmd.Preset != service.NoPresetOption {
		errs = append(errs, svc.ApplyPreset(cmd.Preset))
	}
	switch cmd.Action {
	case "center":
		errs = append(errs, svc.Center())
	case "reset":
		var keys []string
		for _, c := range svc.Snapshot().Controls {
			if c.Group == camera.GroupPicture {
				keys = append(keys, c.Key)
			}
		}
		errs = append(errs, svc.ResetControls(keys))
	}
	if len(cmd.Values) > 0 {
		errs = append(errs, svc.SetControls(cmd.Values))
	}
	if len(cmd.Nudge) > 0 {
		errs = append(errs, svc.Nudge(cmd.Nudge))
	}
	if cmd.Move != nil {
		errs = append(errs, svc.Move(cmd.Move, cmd.MoveTTL))
	}
	return errors.Join(errs...)
}

// Test tries to connect with the given settings and returns the result.
func Test(ctx context.Context, c config.MQTT, password string) error {
	if c.Host == "" {
		return errors.New("broker host is empty")
	}
	u, err := brokerURL(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	cc := clientConfig(session{cfg: c, password: password, deviceID: fmt.Sprintf("test-%d", time.Now().UnixNano())}, Topics{}, u)
	cc.WillMessage = nil
	cc.OnConnectError = func(err error) {
		select {
		case errCh <- err:
		default:
		}
	}
	cm, err := autopaho.NewConnection(ctx, cc)
	if err != nil {
		return err
	}
	defer func() {
		dctx, c2 := context.WithTimeout(context.Background(), time.Second)
		defer c2()
		_ = cm.Disconnect(dctx)
	}()
	waitCh := make(chan error, 1)
	go func() { waitCh <- cm.AwaitConnection(ctx) }()
	select {
	case err := <-errCh:
		return err
	case err := <-waitCh:
		if err != nil {
			return fmt.Errorf("no connection within timeout: %w", err)
		}
		return nil
	}
}
