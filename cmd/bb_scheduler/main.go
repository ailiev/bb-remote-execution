package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"

	remoteexecution "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/buildbarn/bb-remote-execution/pkg/builder"
	"github.com/buildbarn/bb-remote-execution/pkg/proto/scheduler"
	"github.com/buildbarn/bb-storage/pkg/util"
	"github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"google.golang.org/grpc"
)

func main() {
	var (
		jobsPendingMax   = flag.Uint("jobs-pending-max", 100, "Maximum number of build actions to be enqueued")
		webListenAddress = flag.String("web.listen-address", ":80", "Port on which to expose metrics")
		execDigestFuncname = flag.String("execution-digest-function", "sha256", "Digest function reported by the server in its execution capabilities.")
	)
	flag.Parse()

	err := util.UseBinaryLogTempFileSink()
	if err != nil {
		log.Fatalf("Failed to UseBinaryLogTempFileSink: %v", err)
	}

	// Web server for metrics and profiling.
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		log.Fatal(http.ListenAndServe(*webListenAddress, nil))
	}()

	var execDigestFunc remoteexecution.DigestFunction
	switch *execDigestFuncname {
	case "sha256":
		execDigestFunc = remoteexecution.DigestFunction_SHA256
	case "sha1":
		execDigestFunc = remoteexecution.DigestFunction_SHA1
	case "md5":
		execDigestFunc = remoteexecution.DigestFunction_MD5
	default:
		log.Fatalf("Unknown digest function '%s' [from cmd line arg execution-digest-function]", execDigestFuncname)
	}

	executionServer, schedulerServer := builder.NewWorkerBuildQueue(util.DigestKeyWithInstance, execDigestFunc, *jobsPendingMax)

	// RPC server.
	s := grpc.NewServer(
		grpc.StreamInterceptor(grpc_prometheus.StreamServerInterceptor),
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
	)
	remoteexecution.RegisterCapabilitiesServer(s, executionServer)
	remoteexecution.RegisterExecutionServer(s, executionServer)
	scheduler.RegisterSchedulerServer(s, schedulerServer)
	grpc_prometheus.EnableHandlingTimeHistogram()
	grpc_prometheus.Register(s)

	sock, err := net.Listen("tcp", ":8981")
	if err != nil {
		log.Fatal("Failed to create listening socket: ", err)
	}
	if err := s.Serve(sock); err != nil {
		log.Fatal("Failed to serve RPC server: ", err)
	}
}
