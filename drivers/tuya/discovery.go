package tuya

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/fess932/homeLab/internal/netguard"
)

const (
	cmdReqDevInfo = 0x25

	// Больше адресов за один поиск не сканируем: это домашняя сеть, а не /16.
	maxScanHosts = 1024
	scanWorkers  = 128
	scanTimeout  = 700 * time.Millisecond
)

// Ключ UDP-анонсов Tuya общий для всех устройств: он публичен и защищает только от случайного чтения.
var udpKey = func() []byte { k := md5.Sum([]byte("yGAdlopoPVldABfn")); return k[:] }()

// Found — устройство Tuya, найденное в локальной сети.
type Found struct {
	IP        string `json:"ip"`
	DeviceID  string `json:"device_id"`
	Version   string `json:"version"`
	ProductID string `json:"product_id"`
	// broadcast — устройство ответило на запрос сам (есть id и версия);
	// scan — найден только открытый порт 6668, id придётся ввести.
	Via string `json:"via"`
}

type lanResult struct {
	Devices   []Found  `json:"devices"`
	Subnets   []string `json:"subnets"`
	Broadcast bool     `json:"broadcast"`
	Warnings  []string `json:"warnings"`
}

// Discover ищет устройства Tuya двумя способами одновременно: рассылает запрос
// приложения на UDP 7000 и слушает анонсы на 6666/6667/7000, а параллельно проверяет
// порт 6668 во всех адресах подсетей. UDP даёт id и версию, но его может не пропустить
// брандмауэр или сеть Docker; TCP-скан работает всегда, но находит только адрес.
func discoverLAN(ctx context.Context, subnets []netip.Prefix, wait time.Duration) lanResult {
	res := lanResult{Devices: []Found{}, Subnets: []string{}, Warnings: []string{}}
	ifaces := localIPv4()
	if len(subnets) == 0 {
		for _, a := range ifaces {
			subnets = append(subnets, scanPrefix(a.prefix))
		}
	}
	for _, p := range subnets {
		res.Subnets = append(res.Subnets, p.String())
	}

	var mu sync.Mutex
	byIP := map[string]*Found{}
	add := func(f Found) {
		mu.Lock()
		defer mu.Unlock()
		if cur, ok := byIP[f.IP]; ok {
			if cur.Via == "scan" && f.Via == "broadcast" {
				*cur = f
			}
			return
		}
		byIP[f.IP] = &f
	}

	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	var wg sync.WaitGroup

	// UDP: слушаем анонсы и раз в 2 секунды повторяем запрос приложения.
	var conns []*net.UDPConn
	for _, port := range []int{6666, 6667, 7000} {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
		if err != nil {
			res.Warnings = append(res.Warnings, "UDP-порт занят, анонсы на нём не слушаются: "+err.Error())
			continue
		}
		conns = append(conns, c)
		wg.Go(func() { listenAnnounces(ctx, c, add) })
	}
	res.Broadcast = len(conns) > 0
	if res.Broadcast {
		wg.Go(func() {
			tick := time.NewTicker(2 * time.Second)
			defer tick.Stop()
			for {
				sendAppBroadcast(ifaces)
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
			}
		})
	}

	// TCP: открытый 6668 — признак локального протокола Tuya.
	wg.Go(func() {
		for _, ip := range scanTCP(ctx, subnets) {
			add(Found{IP: ip, Via: "scan"})
		}
	})

	<-ctx.Done()
	for _, c := range conns {
		_ = c.Close()
	}
	wg.Wait()

	for _, f := range byIP {
		res.Devices = append(res.Devices, *f)
	}
	slices.SortFunc(res.Devices, func(a, b Found) int {
		return netip.MustParseAddr(a.IP).Compare(netip.MustParseAddr(b.IP))
	})
	return res
}

type ifaceAddr struct {
	ip     netip.Addr
	prefix netip.Prefix
}

func localIPv4() []ifaceAddr {
	var out []ifaceAddr
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(n.IP.To4())
			if !ok || !ip.IsPrivate() {
				continue
			}
			ones, _ := n.Mask.Size()
			out = append(out, ifaceAddr{ip: ip, prefix: netip.PrefixFrom(ip, ones).Masked()})
		}
	}
	return out
}

// scanPrefix сужает сеть интерфейса до /24 вокруг своего адреса: в домашних сетях
// шире не бывает, а /16 сканировался бы минуты.
func scanPrefix(p netip.Prefix) netip.Prefix {
	if p.Bits() >= 24 {
		return p
	}
	return netip.PrefixFrom(p.Addr(), 24).Masked()
}

func broadcastAddr(p netip.Prefix) netip.Addr {
	b := p.Masked().Addr().As4()
	host := uint32(1)<<(32-p.Bits()) - 1
	v := binary.BigEndian.Uint32(b[:]) | host
	binary.BigEndian.PutUint32(b[:], v)
	return netip.AddrFrom4(b)
}

func sendAppBroadcast(ifaces []ifaceAddr) {
	targets := map[netip.Addr]netip.Addr{netip.AddrFrom4([4]byte{255, 255, 255, 255}): {}}
	for _, a := range ifaces {
		targets[broadcastAddr(a.prefix)] = a.ip
	}
	for dst, src := range targets {
		payload, _ := json.Marshal(map[string]string{"from": "app", "ip": src.String()})
		frame, err := frame6699(udpKey, 0, cmdReqDevInfo, payload)
		if err != nil {
			return
		}
		var laddr *net.UDPAddr
		if src.IsValid() {
			laddr = &net.UDPAddr{IP: src.AsSlice()}
		}
		c, err := net.DialUDP("udp4", laddr, &net.UDPAddr{IP: dst.AsSlice(), Port: 7000})
		if err != nil {
			continue
		}
		_, _ = c.Write(frame)
		_ = c.Close()
	}
}

func listenAnnounces(ctx context.Context, c *net.UDPConn, add func(Found)) {
	buf := make([]byte, 4096)
	for ctx.Err() == nil {
		n, _, err := c.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if f, ok := parseAnnounce(buf[:n]); ok {
			add(f)
		}
	}
}

// parseAnnounce разбирает UDP-анонс устройства: 0x55AA открытым текстом (6666),
// 0x55AA с AES-ECB (6667) или 0x6699 с AES-GCM (7000, протокол 3.5).
func parseAnnounce(data []byte) (Found, bool) {
	payload, err := decodeAnnounce(data)
	if err != nil {
		return Found{}, false
	}
	var msg struct {
		IP         string `json:"ip"`
		GwID       string `json:"gwId"`
		Version    string `json:"version"`
		ProductKey string `json:"productKey"`
		From       string `json:"from"`
	}
	if json.Unmarshal(bytes.TrimRight(payload, "\x00"), &msg) != nil || msg.From == "app" || msg.GwID == "" {
		return Found{}, false
	}
	if _, err := netip.ParseAddr(msg.IP); err != nil {
		return Found{}, false
	}
	return Found{IP: msg.IP, DeviceID: msg.GwID, Version: msg.Version, ProductID: msg.ProductKey, Via: "broadcast"}, true
}

func decodeAnnounce(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, errors.New("короткий пакет")
	}
	switch binary.BigEndian.Uint32(data) {
	case prefix55AA:
		if len(data) < 16 {
			return nil, errors.New("короткий пакет")
		}
		n := int(binary.BigEndian.Uint32(data[12:]))
		if n < 12 || 16+n > len(data) {
			return nil, errors.New("повреждённый пакет")
		}
		payload := data[20 : 16+n-8] // без кода возврата и CRC
		if len(payload) > 0 && payload[0] == '{' {
			return payload, nil
		}
		return ecbDecrypt(udpKey, payload)
	case prefix6699:
		if len(data) < 18 {
			return nil, errors.New("короткий пакет")
		}
		n := int(binary.BigEndian.Uint32(data[14:]))
		if n < 28 || 18+n > len(data) {
			return nil, errors.New("повреждённый пакет")
		}
		gcm, _ := cipher.NewGCM(mustAES(udpKey))
		plain, err := gcm.Open(nil, data[18:30], data[30:18+n], data[4:18])
		if err != nil {
			return nil, err
		}
		if len(plain) > 4 && plain[0] != '{' && plain[4] == '{' {
			plain = plain[4:]
		}
		return plain, nil
	}
	return ecbDecrypt(udpKey, data)
}

func scanTCP(ctx context.Context, subnets []netip.Prefix) []string {
	var hosts []netip.Addr
	for _, p := range subnets {
		p = p.Masked()
		for a := p.Addr().Next(); p.Contains(a) && len(hosts) < maxScanHosts; a = a.Next() {
			if broadcastAddr(p) == a || netguard.Check(a) != nil {
				continue
			}
			hosts = append(hosts, a)
		}
	}
	jobs := make(chan netip.Addr)
	var mu sync.Mutex
	var found []string
	var wg sync.WaitGroup
	d := net.Dialer{Timeout: scanTimeout}
	for range min(scanWorkers, len(hosts)) {
		wg.Go(func() {
			for a := range jobs {
				c, err := d.DialContext(ctx, "tcp", netip.AddrPortFrom(a, 6668).String())
				if err == nil {
					c.Close()
					mu.Lock()
					found = append(found, a.String())
					mu.Unlock()
				}
			}
		})
	}
	for _, a := range hosts {
		select {
		case jobs <- a:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	return found
}
