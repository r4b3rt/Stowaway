/*
 * @Author: ph4ntom
 * @Date: 2021-03-23 18:57:46
 * @LastEditors: ph4ntom
 * @LastEditTime: 2021-04-02 17:17:58
 */
package handler

import (
	"Stowaway/agent/manager"
	"Stowaway/protocol"
	"Stowaway/utils"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type Socks struct {
	Username string
	Password string
}

type Setting struct {
	method       string
	isAuthed     bool
	tcpConnected bool
	isUDP        bool
	success      bool
	tcpConn      net.Conn
	udpListener  *net.UDPConn
}

func NewSocks(username, password string) *Socks {
	socks := new(Socks)
	socks.Username = username
	socks.Password = password
	return socks
}

func (socks *Socks) Start(mgr *manager.Manager, component *protocol.MessageComponent) {
	sMessage := protocol.PrepareAndDecideWhichSProtoToUpper(component.Conn, component.Secret, component.UUID)
	header := &protocol.Header{
		Sender:      component.UUID,
		Accepter:    protocol.ADMIN_UUID,
		MessageType: protocol.SOCKSREADY,
		RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))), // No need to set route when agent send mess to admin
		Route:       protocol.TEMP_ROUTE,
	}

	succMess := &protocol.SocksReady{
		OK: 1,
	}

	failMess := &protocol.SocksReady{
		OK: 0,
	}

	mgrTask := &manager.SocksTask{
		Mode: manager.S_CHECKSOCKSREADY, // to make sure the map is clean
	}
	mgr.SocksManager.TaskChan <- mgrTask
	result := <-mgr.SocksManager.ResultChan
	if !result.OK {
		protocol.ConstructMessage(sMessage, header, failMess)
		sMessage.SendMessage()
		return
	}

	close(mgr.SocksManager.SocksTCPDataChan) // close old chan,to make sure only one routine can get data from mgr.SocksTCPDataChan
	mgr.SocksManager.SocksTCPDataChan = make(chan interface{}, 5)
	go socks.dispathSocksTCPData(mgr, component)

	protocol.ConstructMessage(sMessage, header, succMess)
	sMessage.SendMessage()
}

func (socks *Socks) dispathSocksTCPData(mgr *manager.Manager, component *protocol.MessageComponent) {
	for {
		socksData, ok := <-mgr.SocksManager.SocksTCPDataChan // if new "socks start" command come and call the socks.Start(),then the old chan will be closed and current routine must exit immediately
		if ok {
			switch socksData.(type) {
			case *protocol.SocksTCPData:
				message := socksData.(*protocol.SocksTCPData)
				mgrTask := &manager.SocksTask{
					Mode: manager.S_GETTCPDATACHAN,
					Seq:  message.Seq,
				}
				mgr.SocksManager.TaskChan <- mgrTask
				result := <-mgr.SocksManager.ResultChan

				result.DataChan <- message.Data
				mgr.SocksManager.Done <- true

				// if not exist
				if !result.SocksSeqExist {
					go socks.handleSocks(mgr, component, result.DataChan, message.Seq)
				}
			case *protocol.SocksTCPFin:
				message := socksData.(*protocol.SocksTCPFin)
				mgrTask := &manager.SocksTask{
					Mode: manager.S_CLOSETCP,
					Seq:  message.Seq,
				}
				mgr.SocksManager.TaskChan <- mgrTask
			}
		} else {
			return
		}
	}
}

func DispathSocksUDPData(mgr *manager.Manager) {
	for {
		data := <-mgr.SocksManager.SocksUDPDataChan
		switch data.(type) {
		case *protocol.SocksUDPData:
			message := data.(*protocol.SocksUDPData)

			mgrTask := &manager.SocksTask{
				Mode: manager.S_GETUDPCHANS,
				Seq:  message.Seq,
			}
			mgr.SocksManager.TaskChan <- mgrTask
			result := <-mgr.SocksManager.ResultChan

			if result.OK {
				result.DataChan <- message.Data
			}

			mgr.SocksManager.Done <- true
		case *protocol.UDPAssRes:
			message := data.(*protocol.UDPAssRes)

			mgrTask := &manager.SocksTask{
				Mode: manager.S_GETUDPCHANS,
				Seq:  message.Seq,
			}
			mgr.SocksManager.TaskChan <- mgrTask
			result := <-mgr.SocksManager.ResultChan

			if result.OK {
				result.ReadyChan <- message.Addr
			}

			mgr.SocksManager.Done <- true
		}
	}
}

func (socks *Socks) handleSocks(mgr *manager.Manager, component *protocol.MessageComponent, dataChan chan []byte, seq uint64) {
	setting := new(Setting)

	sMessage := protocol.PrepareAndDecideWhichSProtoToUpper(component.Conn, component.Secret, component.UUID)

	defer func() { // no matter what happened, after the function return,tell admin that works done
		finHeader := &protocol.Header{
			Sender:      component.UUID,
			Accepter:    protocol.ADMIN_UUID,
			MessageType: protocol.SOCKSTCPFIN,
			RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))), // No need to set route when agent send mess to admin
			Route:       protocol.TEMP_ROUTE,
		}

		finMess := &protocol.SocksTCPFin{
			Seq: seq,
		}

		protocol.ConstructMessage(sMessage, finHeader, finMess)
		sMessage.SendMessage()
	}()

	for {
		if setting.isAuthed == false && setting.method == "" {
			data, ok := <-dataChan
			if !ok { //重连后原先引用失效，当chan释放后，若不捕捉，会无限循环
				return
			}
			socks.checkMethod(component, setting, data, seq)
		} else if setting.isAuthed == false && setting.method == "PASSWORD" {
			data, ok := <-dataChan
			if !ok {
				return
			}

			socks.auth(component, setting, data, seq)
		} else if setting.isAuthed == true && setting.tcpConnected == false && !setting.isUDP {
			data, ok := <-dataChan
			if !ok {
				return
			}

			socks.buildConn(mgr, component, setting, data, seq)

			if setting.tcpConnected == false && !setting.isUDP {
				return
			}
		} else if setting.isAuthed == true && setting.tcpConnected == true && !setting.isUDP { //All done!
			go proxyC2STCP(setting.tcpConn, dataChan)
			proxyS2CTCP(component, setting.tcpConn, seq)
			return
		} else if setting.isAuthed == true && setting.isUDP && setting.success {
			go proxyC2SUDP(mgr, setting.udpListener, seq)
			proxyS2CUDP(mgr, component, setting.udpListener, seq)
			return
		} else {
			return
		}
	}
}

func (socks *Socks) checkMethod(component *protocol.MessageComponent, setting *Setting, data []byte, seq uint64) {
	sMessage := protocol.PrepareAndDecideWhichSProtoToUpper(component.Conn, component.Secret, component.UUID)

	header := &protocol.Header{
		Sender:      component.UUID,
		Accepter:    protocol.ADMIN_UUID,
		MessageType: protocol.SOCKSTCPDATA,
		RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))), // No need to set route when agent send mess to admin
		Route:       protocol.TEMP_ROUTE,
	}

	failMess := &protocol.SocksTCPData{
		Seq:     seq,
		DataLen: uint64(len([]byte{0x05, 0xff})),
		Data:    []byte{0x05, 0xff},
	}

	noneMess := &protocol.SocksTCPData{
		Seq:     seq,
		DataLen: uint64(len([]byte{0x05, 0x00})),
		Data:    []byte{0x05, 0x00},
	}

	passMess := &protocol.SocksTCPData{
		Seq:     seq,
		DataLen: uint64(len([]byte{0x05, 0x02})),
		Data:    []byte{0x05, 0x02},
	}

	// avoid the scenario that we can get full socks protocol header (rarely happen,just in case)
	defer func() {
		if r := recover(); r != nil {
			setting.method = "ILLEGAL"
		}
	}()

	if data[0] == 0x05 {
		nMethods := int(data[1])

		var supportMethodFinded, userPassFinded, noAuthFinded bool

		for _, method := range data[2 : 2+nMethods] {
			if method == 0x00 {
				noAuthFinded = true
				supportMethodFinded = true
			} else if method == 0x02 {
				userPassFinded = true
				supportMethodFinded = true
			}
		}

		if !supportMethodFinded {
			protocol.ConstructMessage(sMessage, header, failMess)
			sMessage.SendMessage()
			setting.method = "ILLEGAL"
			return
		}

		if noAuthFinded && (socks.Username == "" && socks.Password == "") {
			protocol.ConstructMessage(sMessage, header, noneMess)
			sMessage.SendMessage()
			setting.method = "NONE"
			setting.isAuthed = true
			return
		} else if userPassFinded && (socks.Username != "" && socks.Password != "") {
			protocol.ConstructMessage(sMessage, header, passMess)
			sMessage.SendMessage()
			setting.method = "PASSWORD"
			return
		} else {
			protocol.ConstructMessage(sMessage, header, failMess)
			sMessage.SendMessage()
			setting.method = "ILLEGAL"
			return
		}
	}
	// send nothing
	setting.method = "ILLEGAL"
}

func (socks *Socks) auth(component *protocol.MessageComponent, setting *Setting, data []byte, seq uint64) {
	sMessage := protocol.PrepareAndDecideWhichSProtoToUpper(component.Conn, component.Secret, component.UUID)

	header := &protocol.Header{
		Sender:      component.UUID,
		Accepter:    protocol.ADMIN_UUID,
		MessageType: protocol.SOCKSTCPDATA,
		RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))),
		Route:       protocol.TEMP_ROUTE,
	}

	failMess := &protocol.SocksTCPData{
		Seq:     seq,
		DataLen: uint64(len([]byte{0x01, 0x01})),
		Data:    []byte{0x01, 0x01},
	}

	succMess := &protocol.SocksTCPData{
		Seq:     seq,
		DataLen: uint64(len([]byte{0x01, 0x00})),
		Data:    []byte{0x01, 0x00},
	}

	defer func() {
		if r := recover(); r != nil {
			setting.isAuthed = false
		}
	}()

	ulen := int(data[1])
	slen := int(data[2+ulen])
	clientName := string(data[2 : 2+ulen])
	clientPass := string(data[3+ulen : 3+ulen+slen])

	if clientName != socks.Username || clientPass != socks.Password {
		protocol.ConstructMessage(sMessage, header, failMess)
		sMessage.SendMessage()
		setting.isAuthed = false
		return
	}
	// username && password all fits!
	protocol.ConstructMessage(sMessage, header, succMess)
	sMessage.SendMessage()
	setting.isAuthed = true
}

func (socks *Socks) buildConn(mgr *manager.Manager, component *protocol.MessageComponent, setting *Setting, data []byte, seq uint64) {
	sMessage := protocol.PrepareAndDecideWhichSProtoToUpper(component.Conn, component.Secret, component.UUID)

	header := &protocol.Header{
		Sender:      component.UUID,
		Accepter:    protocol.ADMIN_UUID,
		MessageType: protocol.SOCKSTCPDATA,
		RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))), // No need to set route when agent send mess to admin
		Route:       protocol.TEMP_ROUTE,
	}

	failMess := &protocol.SocksTCPData{
		Seq:     seq,
		DataLen: uint64(len([]byte{0x05, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})),
		Data:    []byte{0x05, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	}

	length := len(data)

	if length <= 2 {
		protocol.ConstructMessage(sMessage, header, failMess)
		sMessage.SendMessage()
		return
	}

	if data[0] == 0x05 {
		switch data[1] {
		case 0x01:
			TCPConnect(mgr, component, setting, data, seq, length)
		case 0x02:
			TCPBind(mgr, component, setting, data, seq, length)
		case 0x03:
			UDPAssociate(mgr, component, setting, data, seq, length)
		default:
			protocol.ConstructMessage(sMessage, header, failMess)
			sMessage.SendMessage()
		}
	}
}

// TCPConnect 如果是代理tcp
func TCPConnect(mgr *manager.Manager, component *protocol.MessageComponent, setting *Setting, data []byte, seq uint64, length int) {
	var host string
	var err error

	sMessage := protocol.PrepareAndDecideWhichSProtoToUpper(component.Conn, component.Secret, component.UUID)

	header := &protocol.Header{
		Sender:      component.UUID,
		Accepter:    protocol.ADMIN_UUID,
		MessageType: protocol.SOCKSTCPDATA,
		RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))),
		Route:       protocol.TEMP_ROUTE,
	}

	failMess := &protocol.SocksTCPData{
		Seq:     seq,
		DataLen: uint64(len([]byte{0x05, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})),
		Data:    []byte{0x05, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	}

	succMess := &protocol.SocksTCPData{
		Seq:     seq,
		DataLen: uint64(len([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})),
		Data:    []byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	}

	defer func() {
		if r := recover(); r != nil {
			setting.tcpConnected = false
		}
	}()

	switch data[3] {
	case 0x01:
		host = net.IPv4(data[4], data[5], data[6], data[7]).String()
	case 0x03:
		host = string(data[5 : length-2])
	case 0x04:
		host = net.IP{data[4], data[5], data[6], data[7],
			data[8], data[9], data[10], data[11], data[12],
			data[13], data[14], data[15], data[16], data[17],
			data[18], data[19]}.String()
	default:
		protocol.ConstructMessage(sMessage, header, failMess)
		sMessage.SendMessage()
		setting.tcpConnected = false
		return
	}

	port := utils.Int2Str(int(data[length-2])<<8 | int(data[length-1]))

	setting.tcpConn, err = net.DialTimeout("tcp", net.JoinHostPort(host, port), 10*time.Second)

	if err != nil {
		protocol.ConstructMessage(sMessage, header, failMess)
		sMessage.SendMessage()
		setting.tcpConnected = false
		return
	}

	mgrTask := &manager.SocksTask{
		Mode:        manager.S_UPDATETCP,
		Seq:         seq,
		SocksSocket: setting.tcpConn,
	}
	mgr.SocksManager.TaskChan <- mgrTask
	socksResult := <-mgr.SocksManager.ResultChan
	if !socksResult.OK { // if admin has already send fin,then close the conn and set setting.tcpConnected -> false
		setting.tcpConn.Close()
		protocol.ConstructMessage(sMessage, header, failMess)
		sMessage.SendMessage()
		setting.tcpConnected = false
		return
	}

	protocol.ConstructMessage(sMessage, header, succMess)
	sMessage.SendMessage()
	setting.tcpConnected = true
}

func proxyC2STCP(conn net.Conn, dataChan chan []byte) {
	for {
		data, ok := <-dataChan
		if !ok { // no need to send FIN actively
			return
		}
		conn.Write(data)
	}
}

func proxyS2CTCP(component *protocol.MessageComponent, conn net.Conn, seq uint64) {
	sMessage := protocol.PrepareAndDecideWhichSProtoToUpper(component.Conn, component.Secret, component.UUID)

	header := &protocol.Header{
		Sender:      component.UUID,
		Accepter:    protocol.ADMIN_UUID,
		MessageType: protocol.SOCKSTCPDATA,
		RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))), // No need to set route when agent send mess to admin
		Route:       protocol.TEMP_ROUTE,
	}

	buffer := make([]byte, 20480)
	for {
		length, err := conn.Read(buffer)
		if err != nil {
			conn.Close() // close conn immediately
			return
		}

		dataMess := &protocol.SocksTCPData{
			Seq:     seq,
			DataLen: uint64(length),
			Data:    buffer[:length],
		}

		protocol.ConstructMessage(sMessage, header, dataMess)
		sMessage.SendMessage()
	}
}

// TCPBind TCPBind方式
func TCPBind(mgr *manager.Manager, component *protocol.MessageComponent, setting *Setting, data []byte, seq uint64, length int) {
	fmt.Println("Not ready") //limited use, add to Todo
	setting.tcpConnected = false
}

type socksLocalAddr struct {
	Host string
	Port int
}

func (addr *socksLocalAddr) byteArray() []byte {
	bytes := make([]byte, 6)
	copy(bytes[:4], net.ParseIP(addr.Host).To4())
	bytes[4] = byte(addr.Port >> 8)
	bytes[5] = byte(addr.Port % 256)
	return bytes
}

// Based on rfc1928,agent must send message strictly
// UDPAssociate UDPAssociate方式
func UDPAssociate(mgr *manager.Manager, component *protocol.MessageComponent, setting *Setting, data []byte, seq uint64, length int) {
	setting.isUDP = true

	sMessage := protocol.PrepareAndDecideWhichSProtoToUpper(component.Conn, component.Secret, component.UUID)

	dataHeader := &protocol.Header{
		Sender:      component.UUID,
		Accepter:    protocol.ADMIN_UUID,
		MessageType: protocol.SOCKSTCPDATA,
		RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))),
		Route:       protocol.TEMP_ROUTE,
	}

	assHeader := &protocol.Header{
		Sender:      component.UUID,
		Accepter:    protocol.ADMIN_UUID,
		MessageType: protocol.UDPASSSTART,
		RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))),
		Route:       protocol.TEMP_ROUTE,
	}

	failMess := &protocol.SocksTCPData{
		Seq:     seq,
		DataLen: uint64(len([]byte{0x05, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})),
		Data:    []byte{0x05, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	}

	defer func() {
		if r := recover(); r != nil {
			setting.success = false
		}
	}()

	var host string
	switch data[3] {
	case 0x01:
		host = net.IPv4(data[4], data[5], data[6], data[7]).String()
	case 0x03:
		host = string(data[5 : length-2])
	case 0x04:
		host = net.IP{data[4], data[5], data[6], data[7],
			data[8], data[9], data[10], data[11], data[12],
			data[13], data[14], data[15], data[16], data[17],
			data[18], data[19]}.String()
	default:
		protocol.ConstructMessage(sMessage, dataHeader, failMess)
		sMessage.SendMessage()
		setting.success = false
		return
	}

	port := utils.Int2Str(int(data[length-2])<<8 | int(data[length-1])) //先拿到客户端想要发送数据的ip:port地址

	udpListenerAddr, err := net.ResolveUDPAddr("udp", "0.0.0.0:0")
	if err != nil {
		protocol.ConstructMessage(sMessage, dataHeader, failMess)
		sMessage.SendMessage()
		setting.success = false
		return
	}

	udpListener, err := net.ListenUDP("udp", udpListenerAddr)
	if err != nil {
		protocol.ConstructMessage(sMessage, dataHeader, failMess)
		sMessage.SendMessage()
		setting.success = false
		return
	}

	sourceAddr := net.JoinHostPort(host, port)

	mgrTask := &manager.SocksTask{
		Mode:          manager.S_UPDATEUDP,
		Seq:           seq,
		SocksListener: udpListener,
	}

	mgr.SocksManager.TaskChan <- mgrTask
	socksResult := <-mgr.SocksManager.ResultChan
	if !socksResult.OK {
		udpListener.Close() // close listener,because tcp conn is closed
		protocol.ConstructMessage(sMessage, dataHeader, failMess)
		sMessage.SendMessage()
		setting.success = false
		return
	}

	mgrTask = &manager.SocksTask{
		Mode: manager.S_GETUDPCHANS,
		Seq:  seq,
	}
	mgr.SocksManager.TaskChan <- mgrTask
	socksResult = <-mgr.SocksManager.ResultChan
	mgr.SocksManager.Done <- true // give true immediately,cuz no need to ensure closeTCP() must after "readyChan := socksResult.ReadyChan" operation,trying to read data from a closed chan won't cause panic

	if !socksResult.OK { // no need to close listener,cuz TCPFIN has helped us
		protocol.ConstructMessage(sMessage, dataHeader, failMess)
		sMessage.SendMessage()
		setting.success = false
		return
	}

	readyChan := socksResult.ReadyChan

	assMess := &protocol.UDPAssStart{
		Seq:           seq,
		SourceAddrLen: uint16(len([]byte(sourceAddr))),
		SourceAddr:    sourceAddr,
	}

	protocol.ConstructMessage(sMessage, assHeader, assMess)
	sMessage.SendMessage()

	if adminResponse, ok := <-readyChan; adminResponse != "" && ok {
		temp := strings.Split(adminResponse, ":")
		adminAddr := temp[0]
		adminPort, _ := strconv.Atoi(temp[1])

		localAddr := socksLocalAddr{adminAddr, adminPort}
		buf := make([]byte, 10)
		copy(buf, []byte{0x05, 0x00, 0x00, 0x01})
		copy(buf[4:], localAddr.byteArray())

		dataMess := &protocol.SocksTCPData{
			Seq:     seq,
			DataLen: 10,
			Data:    buf,
		}

		protocol.ConstructMessage(sMessage, dataHeader, dataMess)
		sMessage.SendMessage()

		setting.udpListener = udpListener
		setting.success = true
		return
	}

	protocol.ConstructMessage(sMessage, dataHeader, failMess)
	sMessage.SendMessage()
	setting.success = false
}

// proxyC2SUDP 代理C-->Sudp流量
func proxyC2SUDP(mgr *manager.Manager, listener *net.UDPConn, seq uint64) {
	mgrTask := &manager.SocksTask{
		Mode: manager.S_GETUDPCHANS,
		Seq:  seq,
	}
	mgr.SocksManager.TaskChan <- mgrTask
	result := <-mgr.SocksManager.ResultChan
	mgr.SocksManager.Done <- true
	// no need to check if OK,cuz if not,"data, ok := <-dataChan" will help us to exit
	dataChan := result.DataChan

	defer func() {
		// Just avoid panic
		if r := recover(); r != nil {
			go func() { //continue to read channel,avoid some remaining data sended by admin blocking our dispatcher
				for {
					_, ok := <-dataChan
					if !ok {
						return
					}
				}
			}()
		}
	}()

	for {
		var remote string
		var udpData []byte

		data, ok := <-dataChan
		if !ok {
			return
		}

		buf := []byte(data)

		if buf[0] != 0x00 || buf[1] != 0x00 || buf[2] != 0x00 {
			continue
		}

		udpHeader := make([]byte, 0, 1024)
		addrtype := buf[3]

		if addrtype == 0x01 { //IPV4
			ip := net.IPv4(buf[4], buf[5], buf[6], buf[7])
			remote = fmt.Sprintf("%s:%d", ip.String(), uint(buf[8])<<8+uint(buf[9]))
			udpData = buf[10:]
			udpHeader = append(udpHeader, buf[:10]...)
		} else if addrtype == 0x03 { //DOMAIN
			nmlen := int(buf[4])
			nmbuf := buf[5 : 5+nmlen+2]
			remote = fmt.Sprintf("%s:%d", nmbuf[:nmlen], uint(nmbuf[nmlen])<<8+uint(nmbuf[nmlen+1]))
			udpData = buf[8+nmlen:]
			udpHeader = append(udpHeader, buf[:8+nmlen]...)
		} else if addrtype == 0x04 { //IPV6
			ip := net.IP{buf[4], buf[5], buf[6], buf[7],
				buf[8], buf[9], buf[10], buf[11], buf[12],
				buf[13], buf[14], buf[15], buf[16], buf[17],
				buf[18], buf[19]}
			remote = fmt.Sprintf("[%s]:%d", ip.String(), uint(buf[20])<<8+uint(buf[21]))
			udpData = buf[22:]
			udpHeader = append(udpHeader, buf[:22]...)
		} else {
			continue
		}

		remoteAddr, err := net.ResolveUDPAddr("udp", remote)
		if err != nil {
			continue
		}

		mgrTask = &manager.SocksTask{
			Mode:            manager.S_UPDATEUDPHEADER,
			SocksHeaderAddr: remote,
			SocksHeader:     udpHeader,
		}
		mgr.SocksManager.TaskChan <- mgrTask
		<-mgr.SocksManager.ResultChan

		listener.WriteToUDP(udpData, remoteAddr)
	}
}

// proxyS2CUDP 代理S-->Cudp流量
func proxyS2CUDP(mgr *manager.Manager, component *protocol.MessageComponent, listener *net.UDPConn, seq uint64) {
	sMessage := protocol.PrepareAndDecideWhichSProtoToUpper(component.Conn, component.Secret, component.UUID)

	header := &protocol.Header{
		Sender:      component.UUID,
		Accepter:    protocol.ADMIN_UUID,
		MessageType: protocol.SOCKSUDPDATA,
		RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))),
		Route:       protocol.TEMP_ROUTE,
	}

	buffer := make([]byte, 20480)
	var data []byte
	var finalLength int

	for {
		length, addr, err := listener.ReadFromUDP(buffer)
		if err != nil {
			listener.Close()
			return
		}

		mgrTask := &manager.SocksTask{
			Mode:            manager.S_GETUDPHEADER,
			SocksHeaderAddr: addr.String(),
		}
		mgr.SocksManager.TaskChan <- mgrTask
		result := <-mgr.SocksManager.ResultChan
		if result.OK {
			finalLength = len(result.SocksUDPHeader) + length
			data = make([]byte, 0, finalLength)
			data = append(data, result.SocksUDPHeader...)
			data = append(data, buffer[:length]...)
		} else {
			return
		}

		dataMess := &protocol.SocksUDPData{
			Seq:     seq,
			DataLen: uint64(finalLength),
			Data:    data,
		}

		protocol.ConstructMessage(sMessage, header, dataMess)
		sMessage.SendMessage()
	}
}
