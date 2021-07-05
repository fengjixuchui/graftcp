package main

// #cgo LDFLAGS: -L../../.. -lgraftcp
//
// #include <stdlib.h>
//
// static void *alloc_string_slice(int len) {
//              return malloc(sizeof(char*)*len);
// }
//
// int client_main(int argc, char **argv);
import "C"
import (
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/hmgle/graftcp/local"
	"github.com/pborman/getopt/v2"
)

const (
	maxArgsLen = 0xfff
)

func clientMain(args []string) int {
	argc := C.int(len(args))

	log.Printf("Got %v args: %v\n", argc, args)

	argv := (*[maxArgsLen]*C.char)(C.alloc_string_slice(argc))
	defer C.free(unsafe.Pointer(argv))

	for i, arg := range args {
		argv[i] = C.CString(arg)
		defer C.free(unsafe.Pointer(argv[i]))
	}

	returnValue := C.client_main(argc, (**C.char)(unsafe.Pointer(argv)))
	return int(returnValue)
}

func constructArgs(args []string) ([]string, []string) {
	server := make([]string, 0, len(args))
	client := make([]string, 0, len(args))

	isCommand := false
	for _, arg := range args {
		if isCommand {
			client = append(client, arg)
		} else {
			if strings.HasPrefix(arg, "--server") {
				server = append(server, strings.TrimPrefix(arg, "--server"))
			} else if strings.HasPrefix(arg, "--client") {
				client = append(client, strings.TrimPrefix(arg, "--client"))
			} else {
				isCommand = true
				client = append(client, arg)
			}
		}
	}

	return server, client
}

var (
	confPath        string
	httpProxyAddr   string
	logFile         string
	logLevel        int8
	selectProxyMode string = "auto"
	socks5Addr      string = "127.0.0.1:1080"
	socks5User      string
	socks5Pwd       string

	blackIPFile    string
	whiteIPFile    string
	notIgnoreLocal bool
)

func init() {
	getopt.FlagLong(&confPath, "config", 0, "Path to the configuration file")
	getopt.FlagLong(&httpProxyAddr, "http_proxy", 0, "http proxy address, e.g.: 127.0.0.1:8080")
	getopt.FlagLong(&selectProxyMode, "select_proxy_mode", 0, "Set the mode for select a proxy [auto | random | only_http_proxy | only_socks5 | direct]")
	getopt.FlagLong(&socks5Addr, "socks5", 0, "SOCKS5 address")
	getopt.FlagLong(&socks5User, "socks5_username", 0, "SOCKS5 username")
	getopt.FlagLong(&socks5Pwd, "socks5_password", 0, "SOCKS5 password")

	getopt.FlagLong(&blackIPFile, "blackip-file", 'b', "The IP in black-ip-file will connect direct")
	getopt.FlagLong(&whiteIPFile, "whiteip-file", 'w', "Only redirect the connect that destination ip in the white-ip-file to SOCKS5")
	notIgnoreLocal = *(getopt.BoolLong("not-ignore-local", 'n', "Connecting to local is not changed by default, this option will redirect it to SOCKS5"))
}

func usage() {
	log.Fatalf("Usage: mgraftcp [options] prog [prog-args]\n%v", getopt.CommandLine.UsageLine())
}

func main() {
	getopt.Parse()
	args := getopt.Args()
	if len(args) == 0 {
		usage()
	}

	retCode := 0
	defer func() { os.Exit(retCode) }()

	// todo: we need special handle on args like '--help' which trigger os.Exit
	// todo: randomly set and detect port number if no one specified
	serverArgs, clientArgs := constructArgs(os.Args[1:])

	clientArgs = append(os.Args[:1], clientArgs...)

	// TODO: config args
	l := local.NewLocal(":0", socks5Addr, socks5User, socks5Pwd, httpProxyAddr)

	tmpDir, err := ioutil.TempDir("/tmp", "mgraftcp")
	if err != nil {
		log.Fatalf("ioutil.TempDir err: %s", err.Error())
	}
	defer os.RemoveAll(tmpDir)
	pipePath := tmpDir + "/mgraftcp.fifo"
	syscall.Mkfifo(pipePath, uint32(os.ModePerm))

	l.FifoFd, err = os.OpenFile(pipePath, os.O_RDWR, 0)
	if err != nil {
		log.Fatalf("os.OpenFile(%s) err: %s", pipePath, err.Error())
	}

	go l.UpdateProcessAddrInfo()
	ln, err := l.StartListen()
	if err != nil {
		log.Fatalf("l.StartListen err: %s", err.Error())
	}
	go l.StartService(ln)
	defer ln.Close()

	_, faddr := l.GetFAddr()

	var fixArgs []string
	fixArgs = append(fixArgs, clientArgs[0])
	fixArgs = append(fixArgs, "-p", strconv.Itoa(faddr.Port), "-f", pipePath)
	fixArgs = append(fixArgs, clientArgs[1:]...)
	log.Printf("serverArgs: %+v, fixArgs: %+v\n", serverArgs, fixArgs)
	retCode = clientMain(fixArgs)
}