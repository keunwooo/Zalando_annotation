package main

import (
	"flag"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/zalando/postgres-operator/pkg/controller"
	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
)

//전역변수 선언
var (
	kubeConfigFile string
	outOfCluster   bool
	version        string
	config         spec.ControllerConfig
)

func mustParseDuration(d string) time.Duration {
	duration, err := time.ParseDuration(d)
	if err != nil {
		panic(err)
	}
	return duration
}

//초기 실행
func init() {
	//flag 분석 프로그램의 기존에 다른 곳에서 선언된 변수를 사용해 옵션을 선언
	//플래그 선언 함수에 변수의 포인터를 전달
	//변수,플래그명,기본값,설명

	//flag를 통해서 얻어온다.
	flag.StringVar(&kubeConfigFile, "kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	flag.BoolVar(&outOfCluster, "outofcluster", false, "Whether the operator runs in- our outside of the Kubernetes cluster.")
	flag.BoolVar(&config.NoDatabaseAccess, "nodatabaseaccess", false, "Disable all access to the database from the operator side.")
	flag.BoolVar(&config.NoTeamsAPI, "noteamsapi", false, "Disable all access to the teams API")
	flag.Parse()

	//ControllerConfig type
	config.EnableJsonLogging = os.Getenv("ENABLE_JSON_LOGGING") == "true"

	// CONFIG_MAP_NAME 환경변수 읽어옴
	// name of the config map where the operator should look for its configuration.
	// Must be present.
	//검증
	configMapRawName := os.Getenv("CONFIG_MAP_NAME")
	if configMapRawName != "" {

		err := config.ConfigMapName.Decode(configMapRawName)
		if err != nil {
			log.Fatalf("incorrect config map name: %v", configMapRawName)
		}

		log.Printf("Fully qualified configmap name: %v", config.ConfigMapName)

	}
	// CRD_READY_WAIT_INTERVAL 환경변수 읽어옴 디폴트값 5초
	if crdInterval := os.Getenv("CRD_READY_WAIT_INTERVAL"); crdInterval != "" {
		config.CRDReadyWaitInterval = mustParseDuration(crdInterval)
	} else {
		config.CRDReadyWaitInterval = 4 * time.Second
	}
	// CRD_READY_WAIT_TIMEOUT 환경변수 읽어옴 디폴트값 30초
	if crdTimeout := os.Getenv("CRD_READY_WAIT_TIMEOUT"); crdTimeout != "" {
		config.CRDReadyWaitTimeout = mustParseDuration(crdTimeout)
	} else {
		config.CRDReadyWaitTimeout = 30 * time.Second
	}
}

func main() {
	var err error

	//Json 로깅을 원하면 기본 ASCII 포맷터 대신 JSON으로 로깅한다.
	if config.EnableJsonLogging {
		log.SetFormatter(&log.JSONFormatter{})
	}

	log.SetOutput(os.Stdout)
	log.Printf("Spilo operator %s\n", version)

	//채널 설정
	sigs := make(chan os.Signal, 1)                    // os.signal 타입을 전송할 수 있는 채널을 만든다.
	stop := make(chan struct{})                        //채널 생성
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM) // Push signals into channel
	//syscall.SIGINT는 ctrl+c 에 대한 시그널이고 syscall.SIGTERM은 종료에 대한 시그널

	wg := &sync.WaitGroup{} // Goroutines can add themselves to this to be waited on
	//sync.WaitGroup은 모든 고루틴이 종료될 때까지 대기해야 할 때 사용

	//RestConfig creates REST config
	//kubernetes가 포드에 제공하는 서비스 계정을 사용하는 구성 개체를 반환
	//outOfCluster 이면 Flag로부터 Config 만듬 kubeconfig 파일 경로에서 구성을 빌드하는 도우미 함수
	config.RestConfig, err = k8sutil.RestConfig(kubeConfigFile, outOfCluster)
	if err != nil {
		log.Fatalf("couldn't get REST config: %v", err)
	}

	//controller.go 에 선언 새로운 controller 생성후 반환
	c := controller.NewController(&config, "")

	//operator 시작
	c.Run(stop, wg)

	//이 부분에서 멈춰있는다. os.Interrupt 시그널이 들어오면 sig가 채널로부터 시그널을 받고 shutting down이 출력되고 종료된다.
	sig := <-sigs
	log.Printf("Shutting down... %+v", sig)

	close(stop) // Tell goroutines to stop themselves
	wg.Wait()   // Wait for all to be stopped
}
