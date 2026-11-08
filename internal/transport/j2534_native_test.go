//go:build (darwin || linux) && cgo

package transport

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestJ2534NativeABI(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("C compiler unavailable")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "provider.c")
	library := filepath.Join(dir, "provider.so")
	code := `#include <stdint.h>
#include <string.h>
typedef struct { uint32_t protocol,rx,tx,time,size,extra; unsigned char data[4128]; } Message;
int32_t PassThruOpen(void *name,uint32_t *id) { if(name && strcmp(name,"fake-device")) return 1; *id=7;return 0; }
int32_t PassThruClose(uint32_t id) { return id==7?0:1; }
int32_t PassThruConnect(uint32_t id,uint32_t p,uint32_t f,uint32_t b,uint32_t *channel) {
 if(id!=7||p!=5||f!=0x800||b!=500000)return 1;*channel=9;return 0;
}
int32_t PassThruDisconnect(uint32_t id){return id==9?0:1;}
int32_t PassThruReadMsgs(uint32_t id,Message *m,uint32_t *n,uint32_t timeout) {
 if(id!=9||*n<1)return 1; memset(m,0,sizeof(*m));m->protocol=5;m->size=7;
 m->data[2]=7;m->data[3]=0xe8;m->data[4]=2;m->data[5]=0x50;m->data[6]=1;*n=1;return 0;
}
int32_t PassThruWriteMsgs(uint32_t id,Message *m,uint32_t *n,uint32_t timeout) {
 return (id==9&&m->protocol==5&&m->size==7&&m->data[3]==0xe0&&*n==1&&timeout>0)?0:1;
}
int32_t PassThruStartMsgFilter(uint32_t id,uint32_t type,Message *mask,Message *pattern,Message *flow,uint32_t *filter) {
 if(id!=9||type!=1||mask->size!=4||pattern->size!=4||flow)return 1;
 *filter=pattern->tx==0x100?2:1;return 0;
}
int32_t PassThruStopMsgFilter(uint32_t id,uint32_t filter){return (id==9&&(filter==1||filter==2))?0:1;}
int32_t PassThruReadVersion(uint32_t id,char *fw,char *dll,char *api){strcpy(fw,"fake");strcpy(dll,"fake");strcpy(api,"04.04");return 0;}
int32_t PassThruGetLastError(char *error){strcpy(error,"bad ABI arguments");return 0;}
`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-shared", "-fPIC", "-o", library, source}
	if runtime.GOOS == "darwin" {
		args[0] = "-dynamiclib"
	}
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile provider: %v\n%s", err, out)
	}
	opts := J2534Options{Library: library, Device: "fake-device", Baud: 500000, Bus: 1}
	if status := ProbeJ2534(opts); !status.Ready || status.HardwareProbed {
		t.Fatalf("probe: %+v", status)
	}
	c, err := OpenJ2534(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Send(context.Background(), Frame{Address: 0x7e0, Data: []byte{2, 0x10, 1}, Bus: 1}); err != nil {
		t.Fatal(err)
	}
	frames, err := c.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || frames[0].Address != 0x7e8 || frames[0].Bus != 1 || frames[0].Data[1] != 0x50 {
		t.Fatalf("frames: %+v", frames)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}
