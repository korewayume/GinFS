package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"html/template"
	random "math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"
)

var htmlTmpl string
var secret string

func init() {
	htmlTmpl = `
<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta http-equiv="X-UA-Compatible" content="IE=edge">
    <meta name="viewport" content="width=device-width,initial-scale=1.0">
    <title>下载文件</title>
</head>
<body>
<form action="{{ .action }}" method="post">
    <textarea placeholder="请输入凭证" style="box-sizing: border-box; width: 100%; height: 70vh;" name="Token"></textarea>
    <button style="box-sizing: border-box; width: 100%; height: 10vh;" type="submit" value="submit">提交</button>
</form>
</body>
</html>
`

	secret = RandomString(5)
}

func ipv4FromAddr(addr net.Addr) net.IP {
	var ip net.IP
	switch v := addr.(type) {
	case *net.IPNet:
		ip = v.IP
	case *net.IPAddr:
		ip = v.IP
	}
	if ip == nil || ip.IsLoopback() {
		return nil
	}
	ip = ip.To4()
	if ip == nil {
		return nil
	}
	return ip
}

func siteIPv4() (net.IP, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, addr := range addrs {
			ip := ipv4FromAddr(addr)
			if ip == nil {
				continue
			}
			return ip, nil
		}
	}
	return nil, errors.New("network unavailable")
}

func RandomPortAndBaseUrl() (int, string) {
	r := random.New(random.NewSource(time.Now().UnixNano()))
	port := 0
	for true {
		randPort := r.Intn(65535)
		if randPort > 1024 {
			port = randPort
			break
		}
	}
	ip, err := siteIPv4()
	if err != nil {
		return port, fmt.Sprintf("http://0.0.0.0:%d", port)
	} else {
		return port, fmt.Sprintf("http://%s:%d", ip.String(), port)
	}
}

func RandomString(length int) string {
	bytes := []byte("abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789")
	result := make([]byte, length)
	r := random.New(random.NewSource(time.Now().UnixNano()))
	for i := 0; i < length; i++ {
		result[i] = bytes[r.Intn(len(bytes))]
	}
	return string(result)
}

func ServeFile(filePath, uri string) gin.HandlerFunc {
	filename := path.Base(filePath)
	return func(c *gin.Context) {
		if c.Request.Method == "POST" {
			token := strings.TrimSpace(c.PostForm("Token"))
			if len(token) == 0 {
				c.HTML(http.StatusBadRequest, "default", gin.H{
					"action": uri,
				})
				return
			}
			authorized := strings.EqualFold(token, secret)
			if !authorized {
				c.HTML(http.StatusUnauthorized, "default", gin.H{
					"action": uri,
				})
				return
			}
			c.FileAttachment(filePath, filename)
		} else {
			c.HTML(http.StatusOK, "default", gin.H{
				"action": uri,
			})
		}
	}
}

func RunServer(filePath string) {
	port, url := RandomPortAndBaseUrl()
	uri := fmt.Sprintf("/%s", RandomString(5))

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	t, _ := template.New("default").Parse(htmlTmpl)
	router.SetHTMLTemplate(t)

	router.GET(uri, ServeFile(filePath, uri))
	router.POST(uri, ServeFile(filePath, uri))

	server := &http.Server{
		Addr:           fmt.Sprintf(":%d", port),
		Handler:        router,
		ReadTimeout:    time.Second * 10,
		WriteTimeout:   time.Second * 10,
		MaxHeaderBytes: 1 << 20,
	}
	go server.ListenAndServe()

	fmt.Println(fmt.Sprintf("Secret: %s\n%s%s\n", secret, url, uri))
	ExecuteCommand(fmt.Sprintf("iptables -A INPUT -p tcp --dport %d -j ACCEPT", port))

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)
	sig := <-ch
	fmt.Println("Receive a signal", sig)

	ExecuteCommand(fmt.Sprintf("iptables -D INPUT -p tcp --dport %d -j ACCEPT", port))

	cxt, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	err := server.Shutdown(cxt)
	if err != nil {
		fmt.Println("Shutdown with error:", err)
		os.Exit(1)
	}
}

func ExecuteCommand(command string) {
	args := strings.Split(command, " ")
	cmd := exec.Command(args[0], args[1:]...)
	var err error
	err = cmd.Run()
	if err != nil {
		fmt.Println("Execute command with error:", err)
		fmt.Println(command)
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Printf("Usage: %s FILE\n", os.Args[0])
		os.Exit(1)
	}
	filePath := os.Args[1]
	if info, err := os.Stat(filePath); os.IsNotExist(err) || info.IsDir() {
		if info.IsDir() {
			fmt.Printf("%s: %s: Is a directory\n", os.Args[0], filePath)
		} else {
			fmt.Printf("%s: %s: No such file\n", os.Args[0], filePath)
		}
		os.Exit(1)
	}

	RunServer(filePath)
}
