// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	reflectionv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

type shopServices []string

func (s shopServices) GetServiceInfo() map[string]grpc.ServiceInfo {
	out := map[string]grpc.ServiceInfo{}
	for _, n := range s {
		out[n] = grpc.ServiceInfo{}
	}
	return out
}

// startShopServer runs a gRPC server for shop.Shop: Get answers {name:"item-<id>"},
// Fail answers NOT_FOUND. It offers reflection, so a request needs no .proto.
func startShopServer(t *testing.T) string {
	t.Helper()
	c := protocompile.Compiler{Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
		Accessor: protocompile.SourceAccessorFromMap(map[string]string{"shop.proto": `syntax = "proto3";
package shop; message Q { string id = 1; } message A { string name = 1; }
service Shop { rpc Get(Q) returns (A); rpc Fail(Q) returns (A); }`})})}
	compiled, err := c.Compile(t.Context(), "shop.proto")
	if err != nil {
		t.Fatal(err)
	}
	files := &protoregistry.Files{}
	if err := files.RegisterFile(compiled[0]); err != nil {
		t.Fatal(err)
	}
	service := compiled[0].Services().ByName("Shop")
	handler := func(_ any, stream grpc.ServerStream) error {
		full, _ := grpc.MethodFromServerStream(stream)
		method := service.Methods().ByName(protoreflect.Name(strings.TrimPrefix(full, "/shop.Shop/")))
		if method == nil {
			return status.Error(codes.Unimplemented, full)
		}
		in := dynamicpb.NewMessage(method.Input())
		if err := stream.RecvMsg(in); err != nil {
			return err
		}
		id := in.Get(in.Descriptor().Fields().ByName("id")).String()
		if method.Name() == "Fail" {
			return status.Error(codes.NotFound, "no item "+id)
		}
		out := dynamicpb.NewMessage(method.Output())
		out.Set(out.Descriptor().Fields().ByName("name"), protoreflect.ValueOfString("item-"+id))
		return stream.SendMsg(out)
	}
	srv := grpc.NewServer(grpc.UnknownServiceHandler(handler))
	reflectionv1.RegisterServerReflectionServer(srv, reflection.NewServerV1(reflection.ServerOptions{
		Services: shopServices{"shop.Shop"}, DescriptorResolver: files, ExtensionResolver: protoregistry.GlobalTypes}))
	lis, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

func TestSend_grpc_request(t *testing.T) {
	e := newSendEnv(t)
	addr := startShopServer(t)
	request := func(id, name, method, assert string) {
		writeFile(t, filepath.Join(e.dir, "tools", "requests", name+".req.yaml"), "id: "+id+"\nname: "+name+`
versions:
  "1":
    grpc:
      server: grpc://`+addr+`
      method: `+method+`
      body:
        json: '{"id": "7"}'
    assert:
      - code == "`+assert+`"
`)
	}
	request("01JA00000000000000000000T6", "item", "shop.Shop/Get", "OK")
	request("01JA00000000000000000000T7", "missing", "shop.Shop/Fail", "OK")
	request("01JA00000000000000000000T8", "expected-miss", "shop.Shop/Fail", "NOT_FOUND")

	code, out, errOut := e.run(t.Context(), "send", "item")
	failCode, _, failErr := e.run(t.Context(), "send", "missing")
	expectedCode, _, _ := e.run(t.Context(), "send", "expected-miss")

	if code != 0 || !strings.Contains(errOut, "GRPC grpc://"+addr+"/shop.Shop/Get") || !strings.Contains(errOut, "OK") || !strings.Contains(out, `"name": "item-7"`) {
		t.Fatalf("send item: code %d\nstdout: %q\nstderr: %s", code, out, errOut)
	}
	if failCode == 0 || !strings.Contains(failErr, "NOT_FOUND") {
		t.Fatalf("send missing: code %d\nstderr: %s", failCode, failErr)
	}
	if expectedCode != 0 {
		t.Fatalf("a test that expects NOT_FOUND must pass, exit %d", expectedCode)
	}
}
