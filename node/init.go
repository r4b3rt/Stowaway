package node

import (
	"Stowaway/common"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
)

var (
	ControlConnForLowerNodeChan = make(chan net.Conn, 1) //下级节点控制信道
	DataConnForLowerNodeChan    = make(chan net.Conn, 1) //下级节点数据信道
	NewNodeMessageChan          = make(chan []byte, 1)   //新节点加入消息
)

//初始化一个节点连接操作
func StartNodeConn(monitor string, listenPort string, nodeID uint32, key []byte) (net.Conn, net.Conn, uint32, error) {
	controlConnToUpperNode, err := net.Dial("tcp", monitor)
	if err != nil {
		log.Println("[*]Connection refused!")
		return controlConnToUpperNode, controlConnToUpperNode, 11235, err
	}
	respcommand, err := common.ConstructCommand("INIT", listenPort, nodeID, key)
	if err != nil {
		log.Printf("[*]Error occured: %s", err)
	}
	_, err = controlConnToUpperNode.Write(respcommand)
	if err != nil {
		log.Printf("[*]Error occured: %s", err)
	}
	for {
		command, _ := common.ExtractCommand(controlConnToUpperNode, key)
		switch command.Command {
		case "ID":
			nodeID = command.NodeId
		case "ACCEPT":
			switch command.Info {
			case "DATA":
				dataConnToUpperNode, err := net.Dial("tcp", monitor)
				if err != nil {
					log.Printf("[*]Error occured: %s", err)
				}
				return controlConnToUpperNode, dataConnToUpperNode, nodeID, nil
			}
		}
	}
}

//初始化节点监听操作
func StartNodeListen(listenPort string, NodeId uint32, key []byte, reconn bool, single bool) {
	var nodeconnected string = "0.0.0.0"
	var result [1]net.Conn
	var NewNodeMessage []byte

	if listenPort == "" {
		return
	}
	if single { //如果passive重连状态下只有startnode一个节点，没有后续节点的话，直接交给AcceptConnFromUpperNode函数
		for {
			controlConnToAdmin, dataConnToAdmin, _ := AcceptConnFromUpperNode(listenPort, NodeId, key)
			ControlConnForLowerNodeChan <- controlConnToAdmin
			DataConnForLowerNodeChan <- dataConnToAdmin
		}
	}

	//如果passive重连状态下startnode后有节点连接，先执行后续节点的初始化操作，再交给AcceptConnFromUpperNode函数
	listenAddr := fmt.Sprintf("0.0.0.0:%s", listenPort)
	WaitingForLowerNode, err := net.Listen("tcp", listenAddr)

	if err != nil {
		//logrus.Errorf("[*]Cannot listen on port %s", listenPort)
		log.Printf("[*]Cannot listen on port %s", listenPort)
		os.Exit(1)
	}

	for {
		ConnToLowerNode, err := WaitingForLowerNode.Accept() //判断一下是否是合法连接
		if err != nil {
			log.Println("[*]", err)
			continue
		}
		if nodeconnected == "0.0.0.0" {
			command, err := common.ExtractCommand(ConnToLowerNode, key)
			if err != nil {
				log.Println("[*]", err)
				continue
			}
			if command.Command == "INIT" {
				if command.NodeId == 0 {
					respNodeID := NodeId + 1
					respCommand, _ := common.ConstructCommand("ID", "", respNodeID, key)
					_, err := ConnToLowerNode.Write(respCommand)
					NewNodeMessage, _ = common.ConstructCommand("NEW", ConnToLowerNode.RemoteAddr().String(), respNodeID, key)
					if err != nil {
						log.Println("[*]", err)
						continue
					}
					controlConnToLowerNode := ConnToLowerNode
					result[0] = controlConnToLowerNode
					nodeconnected = strings.Split(ConnToLowerNode.RemoteAddr().String(), ":")[0]
					respCommand, _ = common.ConstructCommand("ACCEPT", "DATA", respNodeID, key)
					_, err = ConnToLowerNode.Write(respCommand)
					if err != nil {
						log.Println("[*]", err)
						continue
					}
				} else {
					respCommand, _ := common.ConstructCommand("ID", "", command.NodeId, key)
					_, err := ConnToLowerNode.Write(respCommand)
					if err != nil {
						log.Println("[*]", err)
						continue
					}
					respCommand, _ = common.ConstructCommand("ACCEPT", "DATA", command.NodeId, key)
					_, err = ConnToLowerNode.Write(respCommand)
					if err != nil {
						log.Println("[*]", err)
						continue
					}
					controlConnToLowerNode := ConnToLowerNode
					result[0] = controlConnToLowerNode
					nodeconnected = strings.Split(ConnToLowerNode.RemoteAddr().String(), ":")[0]
				}
			} else {
				log.Println("[*]Illegal connection!")
			}
		} else if nodeconnected == strings.Split(ConnToLowerNode.RemoteAddr().String(), ":")[0] {
			dataConToLowerNode := ConnToLowerNode
			ControlConnForLowerNodeChan <- result[0]
			DataConnForLowerNodeChan <- dataConToLowerNode
			NewNodeMessageChan <- NewNodeMessage
			nodeconnected = "0.0.0.0" //继续接受连接？
			if reconn {
				WaitingForLowerNode.Close()
				for {
					controlConnToAdmin, dataConnToAdmin, _ := AcceptConnFromUpperNode(listenPort, NodeId, key)
					ControlConnForLowerNodeChan <- controlConnToAdmin
					DataConnForLowerNodeChan <- dataConnToAdmin
				}
			}
		} else {
			continue
		}
	}
}

func ConnectNextNode(target string, nodeid uint32, key []byte) {
	var NewNodeMessage []byte

	controlConnToNextNode, err := net.Dial("tcp", target)

	if err != nil {
		log.Println("[*]Connection refused!")
		return
	}

	for {
		command, err := common.ExtractCommand(controlConnToNextNode, key)
		if err != nil {
			log.Println("[*]", err)
			return
		}
		switch command.Command {
		case "INIT":
			respNodeID := nodeid + 1
			respCommand, _ := common.ConstructCommand("ID", "", respNodeID, key)
			_, err := controlConnToNextNode.Write(respCommand)
			NewNodeMessage, _ = common.ConstructCommand("NEW", controlConnToNextNode.RemoteAddr().String(), respNodeID, key)
			if err != nil {
				log.Println("[*]", err)
				continue
			}
		case "IDOK":
			dataConnToNextNode, err := net.Dial("tcp", target)
			if err != nil {
				log.Println("[*]Connection refused!")
				return
			}
			ControlConnForLowerNodeChan <- controlConnToNextNode
			DataConnForLowerNodeChan <- dataConnToNextNode
			NewNodeMessageChan <- NewNodeMessage
			return
		}
	}
}

func AcceptConnFromUpperNode(listenPort string, nodeid uint32, key []byte) (net.Conn, net.Conn, uint32) {
	listenAddr := fmt.Sprintf("0.0.0.0:%s", listenPort)
	WaitingForConn, err := net.Listen("tcp", listenAddr)

	var (
		flag        = false
		history     string
		controlconn [1]net.Conn
	)

	if err != nil {
		log.Printf("[*]Cannot listen on port %s", listenPort)
		os.Exit(1)
	}

	for {
		Comingconn, err := WaitingForConn.Accept()
		if err != nil {
			log.Println("[*]", err)
			continue
		}
		if flag == false {
			respcommand, _ := common.ConstructCommand("INIT", listenPort, nodeid, key)
			Comingconn.Write(respcommand)
			command, _ := common.ExtractCommand(Comingconn, key)
			if command.Command == "ID" {
				nodeid = command.NodeId
				respcommand, _ = common.ConstructCommand("IDOK", "", nodeid, key)
				Comingconn.Write(respcommand)
				flag = true
				history = strings.Split(Comingconn.RemoteAddr().String(), ":")[0]
				controlconn[0] = Comingconn
			} else {
				continue
			}
		} else if history == strings.Split(Comingconn.RemoteAddr().String(), ":")[0] {
			WaitingForConn.Close()
			return controlconn[0], Comingconn, nodeid
		} else {
			continue
		}
	}
}
