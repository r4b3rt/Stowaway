package admin

import (
	"Stowaway/common"
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
)

var CurrentNode uint32

/*-------------------------Node模式下相关代码--------------------------*/
//处理node模式下用户的输入
func HandleNodeCommand(startNodeControlConn net.Conn, NodeID string) {
	nodeid64, _ := strconv.ParseInt(NodeID, 10, 32)
	nodeID := uint32(nodeid64)
	CurrentNode = nodeID //把nodeid提取出来，以供上传/下载文件功能使用

	for {
		AdminCommand := <-AdminCommandChan
		switch AdminCommand[0] {
		case "shell":
			respCommand, err := common.ConstructCommand("SHELL", "", nodeID, AESKey)
			_, err = startNodeControlConn.Write(respCommand)
			if err != nil {
				log.Printf("[*]ERROR OCCURED!: %s", err)
			}
			HandleShellToNode(startNodeControlConn, nodeID)
		case "socks":
			var socksStartData string
			if len(AdminCommand) == 2 {
				socksStartData = fmt.Sprintf("%s:::%s:::%s", AdminCommand[1], "", "")
			} else if len(AdminCommand) == 3 {
				fmt.Println("Illegal username/password! Try again!")
				ReadyChange <- true
				IsShellMode <- true
				break
			} else {
				socksStartData = fmt.Sprintf("%s:::%s:::%s", AdminCommand[1], AdminCommand[2], AdminCommand[3])
			}
			respCommand, err := common.ConstructCommand("SOCKS", socksStartData, nodeID, AESKey)
			_, err = startNodeControlConn.Write(respCommand)
			if err != nil {
				log.Println("[*]StartNode seems offline")
			}
			if <-NodeSocksStarted {
				go StartSocksServiceForClient(AdminCommand, startNodeControlConn, nodeID)
			}
			ReadyChange <- true
			IsShellMode <- true
		case "stopsocks":
			err := SocksListenerForClient.Close()
			if err != nil {
				log.Println("[*]You have never started socks service!")
			}
			respCommand, _ := common.ConstructCommand("SOCKSOFF", " ", nodeID, AESKey)
			_, err = startNodeControlConn.Write(respCommand)
			if err != nil {
				log.Println("[*]StartNode seems offline")
			}
			ReadyChange <- true
			IsShellMode <- true
		case "ssh":
			if len(AdminCommand) == 4 {
				go StartSSHService(startNodeControlConn, AdminCommand, nodeID)
				HandleSSHToNode(startNodeControlConn, nodeID)
			} else {
				fmt.Println("Wrong format! Should be ssh [ip:port] [name] [pass]")
				ReadyChange <- true
				IsShellMode <- true
			}
		case "connect":
			if len(AdminCommand) == 2 {
				respCommand, _ := common.ConstructCommand("CONNECT", AdminCommand[1], nodeID, AESKey)
				startNodeControlConn.Write(respCommand)
			} else {
				fmt.Println("Wrong format! Should be connect [ip:port]")
			}
			ReadyChange <- true
			IsShellMode <- true
		case "upload":
			if len(AdminCommand) == 2 {
				go common.UploadFile(AdminCommand[1], startNodeControlConn, DataConn, nodeID, GetName, AESKey, 0, true)
			} else {
				fmt.Println("Bad format! Should be upload [filename]")
			}
			ReadyChange <- true
			IsShellMode <- true
		case "download":
			if len(AdminCommand) == 2 {
				go common.DownloadFile(AdminCommand[1], startNodeControlConn, nodeID, AESKey)
			} else {
				fmt.Println("Bad format! Should be download [filename]")
			}
			ReadyChange <- true
			IsShellMode <- true
		case "forward":
			if len(AdminCommand) == 3 {
				go StartPortForwardForClient(AdminCommand, DataConn, startNodeControlConn, nodeID, AESKey)
			} else {
				fmt.Println("Bad format! Should be forward [localport] [rhostip]:[rhostport]")
			}
			ReadyChange <- true
			IsShellMode <- true
		case "stopforward":
			go StopForward()
			ReadyChange <- true
			IsShellMode <- true
		case "reflect":
			if len(AdminCommand) == 3 {
				go StartReflectForClient(AdminCommand, DataConn, startNodeControlConn, nodeID, AESKey)
			} else {
				fmt.Println("Bad format! Should be reflect [rhostport] [localport]")
			}
			ReadyChange <- true
			IsShellMode <- true
		case "stopreflect":
			go StopReflect(startNodeControlConn, nodeID)
			ReadyChange <- true
			IsShellMode <- true
		case "help":
			ShowNodeHelp()
			ReadyChange <- true
			IsShellMode <- true
		case "":
			ReadyChange <- true
			IsShellMode <- true
			continue
		case "exit":
			*CliStatus = "admin"
			ReadyChange <- true
			IsShellMode <- true
			return
		default:
			fmt.Println("Illegal command, enter help to get available commands")
			ReadyChange <- true
			IsShellMode <- true
		}
	}
}

/*-------------------------Shell模式下相关代码--------------------------*/
//处理shell开启时的输入
func HandleShellToNode(startNodeControlConn net.Conn, nodeID uint32) {
	inputReader := bufio.NewReader(os.Stdin)
	for {
		command, err := inputReader.ReadString('\n')
		if runtime.GOOS == "windows" {
			command = strings.Replace(command, "\r", "", -1)
		}
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		switch command {
		case "exit\n":
			if nodeID == 1 {
				*CliStatus = "startnode"
			} else {
				*CliStatus = "node " + fmt.Sprint(nodeID)
			}
			respCommand, _ := common.ConstructCommand("SHELL", command, nodeID, AESKey)
			startNodeControlConn.Write(respCommand)
			ReadyChange <- true
			IsShellMode <- true
			return
		default:
			respCommand, _ := common.ConstructCommand("SHELL", command, nodeID, AESKey)
			startNodeControlConn.Write(respCommand)
		}
	}
}

/*-------------------------Ssh模式下相关代码--------------------------*/
//处理ssh开启时的输入
func HandleSSHToNode(startNodeControlConn net.Conn, nodeID uint32) {
	inputReader := bufio.NewReader(os.Stdin)
	log.Println("[*]Waiting for response,please be patient")
	if conrinueornot := <-SshSuccess; conrinueornot {
		fmt.Print("(ssh mode)>>>")
		for {
			command, err := inputReader.ReadString('\n')
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			switch command {
			case "exit\n":
				if nodeID == 1 {
					*CliStatus = "startnode"
				} else {
					*CliStatus = "node " + fmt.Sprint(nodeID)
				}
				respCommand, _ := common.ConstructCommand("SSHCOMMAND", command, nodeID, AESKey)
				startNodeControlConn.Write(respCommand)
				ReadyChange <- true
				IsShellMode <- true
				return
			case "\n":
				fmt.Print("(ssh mode)>>>")
			default:
				respCommand, _ := common.ConstructCommand("SSHCOMMAND", command, nodeID, AESKey)
				startNodeControlConn.Write(respCommand)

			}
		}
	} else {
		return
	}
}
