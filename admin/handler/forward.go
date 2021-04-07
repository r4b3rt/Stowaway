/*
 * @Author: ph4ntom
 * @Date: 2021-04-02 13:22:25
 * @LastEditors: ph4ntom
 * @LastEditTime: 2021-04-03 16:01:31
 */
package handler

import (
	"Stowaway/admin/manager"
	"Stowaway/protocol"
	"fmt"
	"net"
)

type Forward struct {
	Addr string
	Port string
}

func NewForward(port, addr string) *Forward {
	forward := new(Forward)
	forward.Port = port
	forward.Addr = addr
	return forward
}

func (forward *Forward) LetForward(component *protocol.MessageComponent, mgr *manager.Manager, route string, uuid string, uuidNum int) error {
	listenAddr := fmt.Sprintf("0.0.0.0:%s", forward.Port)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}

	sMessage := protocol.PrepareAndDecideWhichSProtoToLower(component.Conn, component.Secret, component.UUID)

	header := &protocol.Header{
		Sender:      protocol.ADMIN_UUID,
		Accepter:    uuid,
		MessageType: protocol.FORWARDTEST,
		RouteLen:    uint32(len([]byte(route))),
		Route:       route,
	}

	testMess := &protocol.ForwardTest{
		AddrLen: uint16(len([]byte(forward.Addr))),
		Addr:    forward.Addr,
	}

	protocol.ConstructMessage(sMessage, header, testMess)
	sMessage.SendMessage()

	if ready := <-mgr.ForwardManager.ForwardReady; !ready {
		listener.Close()
		err := fmt.Errorf("Fail to forward port %s to remote addr %s,remote addr is not responding", forward.Port, forward.Addr)
		return err
	}

	mgrTask := &manager.ForwardTask{
		Mode:     manager.F_NEWFORWARD,
		UUIDNum:  uuidNum,
		Listener: listener,
		Port:     forward.Port,
	}

	mgr.ForwardManager.TaskChan <- mgrTask
	<-mgr.ForwardManager.ResultChan

	go forward.handleForwardListener(component, mgr, listener, route, uuid, uuidNum)

	return nil
}

func (forward *Forward) handleForwardListener(component *protocol.MessageComponent, mgr *manager.Manager, listener net.Listener, route string, uuid string, uuidNum int) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			listener.Close() // todo:map没有释放
			return
		}

		mgrTask := &manager.ForwardTask{
			Mode:    manager.F_GETNEWSEQ,
			UUIDNum: uuidNum,
			Port:    forward.Port,
		}
		mgr.ForwardManager.TaskChan <- mgrTask
		result := <-mgr.ForwardManager.ResultChan
		seq := result.ForwardSeq

		mgrTask = &manager.ForwardTask{
			Mode:    manager.F_ADDCONN,
			UUIDNum: uuidNum,
			Seq:     seq,
			Port:    forward.Port,
			Conn:    conn,
		}
		mgr.ForwardManager.TaskChan <- mgrTask
		result = <-mgr.ForwardManager.ResultChan
		if !result.OK {
			conn.Close()
			return
		}

		go forward.handleForward(component, mgr, conn, route, uuid, uuidNum, seq)
	}
}

func (forward *Forward) handleForward(component *protocol.MessageComponent, mgr *manager.Manager, conn net.Conn, route string, uuid string, uuidNum int, seq uint64) {
	sMessage := protocol.PrepareAndDecideWhichSProtoToLower(component.Conn, component.Secret, component.UUID)
	// tell agent to start
	startHeader := &protocol.Header{
		Sender:      protocol.ADMIN_UUID,
		Accepter:    uuid,
		MessageType: protocol.FORWARDSTART,
		RouteLen:    uint32(len([]byte(route))),
		Route:       route,
	}

	startMess := &protocol.ForwardStart{
		Seq:     seq,
		AddrLen: uint16(len([]byte(forward.Addr))),
		Addr:    forward.Addr,
	}

	protocol.ConstructMessage(sMessage, startHeader, startMess)
	sMessage.SendMessage()

	// begin to work
	defer func() {
		finHeader := &protocol.Header{
			Sender:      protocol.ADMIN_UUID,
			Accepter:    uuid,
			MessageType: protocol.FORWARDFIN,
			RouteLen:    uint32(len([]byte(route))),
			Route:       route,
		}

		finMess := &protocol.ForwardFin{
			Seq: seq,
		}

		protocol.ConstructMessage(sMessage, finHeader, finMess)
		sMessage.SendMessage()
	}()

	mgrTask := &manager.ForwardTask{
		Mode:    manager.F_GETDATACHAN,
		UUIDNum: uuidNum,
		Seq:     seq,
		Port:    forward.Port,
	}
	mgr.ForwardManager.TaskChan <- mgrTask
	result := <-mgr.ForwardManager.ResultChan
	if !result.OK {
		return
	}

	dataChan := result.DataChan

	go func() {
		for {
			if data, ok := <-dataChan; ok {
				conn.Write(data)
			} else {
				return
			}
		}
	}()

	dataHeader := &protocol.Header{
		Sender:      protocol.ADMIN_UUID,
		Accepter:    uuid,
		MessageType: protocol.FORWARDDATA,
		RouteLen:    uint32(len([]byte(route))),
		Route:       route,
	}

	buffer := make([]byte, 20480)

	for {
		length, err := conn.Read(buffer)
		if err != nil {
			conn.Close()
			return
		}

		forwardDataMess := &protocol.ForwardData{
			Seq:     seq,
			DataLen: uint64(length),
			Data:    buffer[:length],
		}

		protocol.ConstructMessage(sMessage, dataHeader, forwardDataMess)
		sMessage.SendMessage()
	}
}

func DispatchForwardData(mgr *manager.Manager) {
	for {
		data := <-mgr.ForwardManager.ForwardDataChan

		switch data.(type) {
		case *protocol.ForwardReady:
			message := data.(*protocol.ForwardReady)
			if message.OK == 1 {
				mgr.ForwardManager.ForwardReady <- true
			} else {
				mgr.ForwardManager.ForwardReady <- false
			}
		case *protocol.ForwardData:
			message := data.(*protocol.ForwardData)
			mgrTask := &manager.ForwardTask{
				Mode: manager.F_GETDATACHAN_WITHOUTUUID,
				Seq:  message.Seq,
			}
			mgr.ForwardManager.TaskChan <- mgrTask
			result := <-mgr.ForwardManager.ResultChan
			if result.OK {
				result.DataChan <- message.Data
			}
			mgr.ForwardManager.Done <- true
		case *protocol.ForwardFin:
			message := data.(*protocol.ForwardFin)
			mgrTask := &manager.ForwardTask{
				Mode: manager.F_CLOSETCP,
				Seq:  message.Seq,
			}
			mgr.ForwardManager.TaskChan <- mgrTask
		}
	}
}
