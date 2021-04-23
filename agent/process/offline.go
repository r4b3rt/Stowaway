package process

import (
	"Stowaway/agent/initial"
	"Stowaway/agent/manager"
	"Stowaway/global"
	"Stowaway/protocol"
	"Stowaway/share"
	"Stowaway/utils"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/libp2p/go-reuseport"
)

func upstreamOffline(mgr *manager.Manager, options *initial.Options) {
	if options.Mode == initial.NORMAL_ACTIVE || options.Mode == initial.PROXY_ACTIVE { // not passive && no reconn,exit immediately
		os.Exit(0)
	}

	forceShutdown(mgr)

	broadcastOfflineMess(mgr)

	if options.Mode == initial.IPTABLES_REUSE_PASSIVE {
		initial.DeletePortReuseRules(options.Listen, options.ReusePort)
	}

	var newConn net.Conn
	switch options.Mode {
	case initial.NORMAL_PASSIVE:
		newConn = normalPassiveReconn(options)
	case initial.IPTABLES_REUSE_PASSIVE:
		newConn = ipTableReusePassiveReconn(options)
	case initial.SO_REUSE_PASSIVE:
		newConn = soReusePassiveReconn(options)
	}

	global.UpdateGComponent(newConn)

	tellAdminReonline(mgr)

	broadcastReonlineMess(mgr)

	return
}

func normalPassiveReconn(options *initial.Options) net.Conn {
	listenAddr, _, err := utils.CheckIPPort(options.Listen)
	if err != nil {
		log.Fatalf("[*]Error occured: %s", err.Error())
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("[*]Error occured: %s", err.Error())
	}

	defer func() {
		listener.Close()
	}()

	var sMessage, rMessage protocol.Message

	hiMess := &protocol.HIMess{
		GreetingLen: uint16(len("Keep slient")),
		Greeting:    "Keep slient",
		UUIDLen:     uint16(len(global.G_Component.UUID)),
		UUID:        global.G_Component.UUID,
		IsAdmin:     0,
		IsReconnect: 1,
	}

	header := &protocol.Header{
		Sender:      protocol.TEMP_UUID,
		Accepter:    protocol.ADMIN_UUID,
		MessageType: protocol.HI,
		RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))),
		Route:       protocol.TEMP_ROUTE,
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("[*]Error occured: %s\n", err.Error())
			conn.Close()
			continue
		}

		if err := share.PassivePreAuth(conn, options.Secret); err != nil {
			log.Fatalf("[*]Error occured: %s", err.Error())
		}

		rMessage = protocol.PrepareAndDecideWhichRProtoFromUpper(conn, options.Secret, protocol.TEMP_UUID)
		fHeader, fMessage, err := protocol.DestructMessage(rMessage)

		if err != nil {
			log.Printf("[*]Fail to set connection from %s, Error: %s\n", conn.RemoteAddr().String(), err.Error())
			conn.Close()
			continue
		}

		if fHeader.MessageType == protocol.HI {
			mmess := fMessage.(*protocol.HIMess)
			if mmess.Greeting == "Shhh..." && mmess.IsAdmin == 1 {
				sMessage = protocol.PrepareAndDecideWhichSProtoToUpper(conn, options.Secret, protocol.TEMP_UUID)
				protocol.ConstructMessage(sMessage, header, hiMess, false)
				sMessage.SendMessage()
				return conn
			}
		}

		conn.Close()
	}
}

func ipTableReusePassiveReconn(options *initial.Options) net.Conn {
	initial.SetPortReuseRules(options.Listen, options.ReusePort)
	return normalPassiveReconn(options)
}

func soReusePassiveReconn(options *initial.Options) net.Conn {
	listenAddr := fmt.Sprintf("%s:%s", options.ReuseHost, options.ReusePort)

	listener, err := reuseport.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("[*]Error occured: %s", err.Error())
	}

	defer func() {
		listener.Close()
	}()

	var sMessage, rMessage protocol.Message

	hiMess := &protocol.HIMess{
		GreetingLen: uint16(len("Keep slient")),
		Greeting:    "Keep slient",
		UUIDLen:     uint16(len(global.G_Component.UUID)),
		UUID:        global.G_Component.UUID,
		IsAdmin:     0,
		IsReconnect: 1,
	}

	header := &protocol.Header{
		Sender:      protocol.TEMP_UUID,
		Accepter:    protocol.ADMIN_UUID,
		MessageType: protocol.HI,
		RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))),
		Route:       protocol.TEMP_ROUTE,
	}

	secret := utils.GetStringMd5(options.Secret)

	for {
		conn, err := listener.Accept()

		if err != nil {
			conn.Close()
			continue
		}

		defer conn.SetReadDeadline(time.Time{})
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))

		buffer := make([]byte, 16)
		count, err := io.ReadFull(conn, buffer)

		if err != nil {
			if timeoutErr, ok := err.(net.Error); ok && timeoutErr.Timeout() {
				go initial.ProxyStream(conn, buffer[:count], options.ReusePort)
				continue
			} else {
				conn.Close()
				continue
			}
		}

		if string(buffer[:count]) == secret[:16] {
			conn.Write([]byte(secret[:16]))
		} else {
			go initial.ProxyStream(conn, buffer[:count], options.ReusePort)
			continue
		}

		rMessage = protocol.PrepareAndDecideWhichRProtoFromUpper(conn, options.Secret, protocol.TEMP_UUID)
		fHeader, fMessage, err := protocol.DestructMessage(rMessage)

		if err != nil {
			log.Printf("[*]Fail to set connection from %s, Error: %s\n", conn.RemoteAddr().String(), err.Error())
			conn.Close()
			continue
		}

		if fHeader.MessageType == protocol.HI {
			mmess := fMessage.(*protocol.HIMess)
			if mmess.Greeting == "Shhh..." && mmess.IsAdmin == 1 {
				sMessage = protocol.PrepareAndDecideWhichSProtoToUpper(conn, options.Secret, protocol.TEMP_UUID)
				protocol.ConstructMessage(sMessage, header, hiMess, false)
				sMessage.SendMessage()
				return conn
			}
		}

		conn.Close()
	}
}

func forceShutdown(mgr *manager.Manager) {
	backwardTask := &manager.BackwardTask{
		Mode: manager.B_FORCESHUTDOWN,
	}
	mgr.BackwardManager.TaskChan <- backwardTask
	<-mgr.BackwardManager.ResultChan

	forwardTask := &manager.ForwardTask{
		Mode: manager.F_FORCESHUTDOWN,
	}
	mgr.ForwardManager.TaskChan <- forwardTask
	<-mgr.ForwardManager.ResultChan

	socksTask := &manager.SocksTask{
		Mode: manager.S_FORCESHUTDOWN,
	}
	mgr.SocksManager.TaskChan <- socksTask
	<-mgr.SocksManager.ResultChan
}

func broadcastOfflineMess(mgr *manager.Manager) {
	childrenTask := &manager.ChildrenTask{
		Mode: manager.C_GETALLCHILDREN,
	}

	mgr.ChildrenManager.TaskChan <- childrenTask
	result := <-mgr.ChildrenManager.ResultChan

	for _, childUUID := range result.Children {
		task := &manager.ChildrenTask{
			Mode: manager.C_GETCONN,
			UUID: childUUID,
		}
		mgr.ChildrenManager.TaskChan <- task
		result = <-mgr.ChildrenManager.ResultChan

		sMessage := protocol.PrepareAndDecideWhichSProtoToUpper(result.Conn, global.G_Component.Secret, global.G_Component.UUID)

		header := &protocol.Header{
			Sender:      global.G_Component.UUID,
			Accepter:    childUUID,
			MessageType: protocol.UPSTREAMOFFLINE,
			RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))),
			Route:       protocol.TEMP_ROUTE,
		}

		offlineMess := &protocol.UpstreamOffline{
			OK: 1,
		}

		protocol.ConstructMessage(sMessage, header, offlineMess, false)
		sMessage.SendMessage()
	}
}

func broadcastReonlineMess(mgr *manager.Manager) {
	childrenTask := &manager.ChildrenTask{
		Mode: manager.C_GETALLCHILDREN,
	}

	mgr.ChildrenManager.TaskChan <- childrenTask
	result := <-mgr.ChildrenManager.ResultChan

	for _, childUUID := range result.Children {
		task := &manager.ChildrenTask{
			Mode: manager.C_GETCONN,
			UUID: childUUID,
		}
		mgr.ChildrenManager.TaskChan <- task
		result = <-mgr.ChildrenManager.ResultChan

		sMessage := protocol.PrepareAndDecideWhichSProtoToUpper(result.Conn, global.G_Component.Secret, global.G_Component.UUID)

		header := &protocol.Header{
			Sender:      global.G_Component.UUID,
			Accepter:    childUUID,
			MessageType: protocol.UPSTREAMREONLINE,
			RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))),
			Route:       protocol.TEMP_ROUTE,
		}

		reOnlineMess := &protocol.UpstreamReonline{
			OK: 1,
		}

		protocol.ConstructMessage(sMessage, header, reOnlineMess, false)
		sMessage.SendMessage()
	}
}

func downStreamOffline(mgr *manager.Manager, options *initial.Options, uuid string) {
	childrenTask := &manager.ChildrenTask{ // del the child
		Mode: manager.C_DELCHILD,
		UUID: uuid,
	}

	mgr.ChildrenManager.TaskChan <- childrenTask
	<-mgr.ChildrenManager.ResultChan

	sMessage := protocol.PrepareAndDecideWhichSProtoToUpper(global.G_Component.Conn, global.G_Component.Secret, global.G_Component.UUID)

	header := &protocol.Header{
		Sender:      global.G_Component.UUID,
		Accepter:    protocol.ADMIN_UUID,
		MessageType: protocol.NODEOFFLINE,
		RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))),
		Route:       protocol.TEMP_ROUTE,
	}

	offlineMess := &protocol.NodeOffline{
		UUIDLen: uint16(len(uuid)),
		UUID:    uuid,
	}

	protocol.ConstructMessage(sMessage, header, offlineMess, false)
	sMessage.SendMessage()
}

func tellAdminReonline(mgr *manager.Manager) {
	childrenTask := &manager.ChildrenTask{
		Mode: manager.C_GETALLCHILDREN,
	}

	mgr.ChildrenManager.TaskChan <- childrenTask
	result := <-mgr.ChildrenManager.ResultChan

	sMessage := protocol.PrepareAndDecideWhichSProtoToUpper(global.G_Component.Conn, global.G_Component.Secret, global.G_Component.UUID)

	reheader := &protocol.Header{
		Sender:      global.G_Component.UUID,
		Accepter:    protocol.ADMIN_UUID,
		MessageType: protocol.NODEREONLINE,
		RouteLen:    uint32(len([]byte(protocol.TEMP_ROUTE))),
		Route:       protocol.TEMP_ROUTE,
	}

	for _, childUUID := range result.Children {
		task := &manager.ChildrenTask{
			Mode: manager.C_GETCONN,
			UUID: childUUID,
		}
		mgr.ChildrenManager.TaskChan <- task
		result = <-mgr.ChildrenManager.ResultChan

		reMess := &protocol.NodeReonline{
			ParentUUIDLen: uint16(len(global.G_Component.UUID)),
			ParentUUID:    global.G_Component.UUID,
			UUIDLen:       uint16(len(childUUID)),
			UUID:          childUUID,
			IPLen:         uint16(len(result.Conn.RemoteAddr().String())),
			IP:            result.Conn.RemoteAddr().String(),
		}

		protocol.ConstructMessage(sMessage, reheader, reMess, false)
		sMessage.SendMessage()
	}
}

func DispatchOfflineMess(agent *Agent) {
	for {
		message := <-agent.mgr.OfflineManager.OfflineMessChan

		switch message.(type) {
		case *protocol.UpstreamOffline:
			forceShutdown(agent.mgr)
			broadcastOfflineMess(agent.mgr)
		case *protocol.UpstreamReonline:
			agent.sendMyInfo()
			tellAdminReonline(agent.mgr)
			broadcastReonlineMess(agent.mgr)
		}
	}
}
