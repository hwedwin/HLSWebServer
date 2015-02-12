// hls.go
package main

/*
#cgo linux CFLAGS: -DLINUX=1
#cgo LDFLAGS: -lTsMux  -L.

void* createH264MuxTs();
void setTsDataCB(void* inst, void* f,void* user_data);
int rawH264Data2Ts(void* inst, void* data, unsigned int len);
void releaseH264MuxTs(void* inst);

extern void goDataTsCallBack();

void setCB(void* handle, void* user_data)
{
    setTsDataCB(handle,goDataTsCallBack, user_data);
}

*/
import "C"

import (
	"bytes"

	"fmt"
	"os"
	"time"
	"unsafe"
)

const (
	TSFILEMAX_DURATION = 60
)

/*
m3u8 文件生成、
#EXTM3U     						 m3u文件头，必须放在第一

#EXT-X-MEDIA-SEQUENCE				 第一个TS分片的序列号

#EXT-X-TARGETDURATION     			 每个分片TS的最大的时长

#EXT-X-ALLOW-CACHE       		     是否允许cache

#EXT-X-ENDLIST          		     m3u8文件结束符

#EXTINF                   		  extra info，分片TS的信息，如时长，带宽等
//EXT-X-STREAM-INF字段，说明了关于所属下载地址的相关信息。
#EXT-X-STREAM-INF:PROGRAM-ID=1, BANDWIDTH=500000

*/

type m3u8Tag struct {
	url       string
	duration  uint32
	bandwidth uint32
	timeStamp uint32
}

type m3u8SliceFile struct {
	fileHandler *os.File
	seq         uint32
	tag         [3]m3u8Tag
	pos         uint32
}

func (this *m3u8SliceFile) Init(ID string) bool {

	var err error
	this.fileHandler, err = os.OpenFile(ID+".m3u8",
		os.O_CREATE|os.O_RDWR,
		0777)
	if err != nil {
		fmt.Println("Init OpenFile Fail", err)
		return false
	}
	this.pos = 0

	return true
}

func (this *m3u8SliceFile) Update(tag m3u8Tag) {
	this.seq += 1
	this.tag[this.pos] = tag
	this.pos += 1
	this.pos %= 3
	buf := new(bytes.Buffer)

	fmt.Fprintf(buf, "#EXTM3U\n"+
		"#EXT-X-MEDIA-SEQUENCE:%d\n"+
		"#EXTINF:%d\n"+
		"%s\n"+
		"#EXTINF:%d\n"+
		"%s\n"+
		"#EXTINF:%d\n"+
		"%s\n"+
		"#EXT-X-ENDLIST", this.seq,
		this.tag[0].duration, this.tag[0].url,
		this.tag[1].duration, this.tag[1].url,
		this.tag[2].duration, this.tag[2].url)
	this.fileHandler.Seek(0, os.SEEK_SET)
	this.fileHandler.Write(buf.Bytes())
	this.fileHandler.Sync()
}

func (this *m3u8SliceFile) Uninit() {
	this.fileHandler.Close()
}

type hlsFile struct {
	fileName    string
	fileHandler *os.File
	fileSize    uint32
	m3u8        *m3u8SliceFile
	begTime     uint32
}

func (this *hlsFile) Init(ID string) {
	this.fileHandler = nil
	this.begTime = 0
	this.m3u8 = new(m3u8SliceFile)
	this.m3u8.Init(ID)
}

func (this *hlsFile) Uninit(ID string) {
	if this.fileHandler != nil {
		this.fileHandler.Close()
	}
	if this.m3u8 != nil {
		this.m3u8.Uninit()
	}
}

func (this *hlsFile) generateFileName() string {

	filename := new(bytes.Buffer)
	now := time.Now()
	fmt.Fprintf(filename, "%04d_%02d_%02d_%02d_%02d_%02d.ts",
		now.Year(),
		now.Month(),
		now.Day(),
		now.Hour(),
		now.Minute(),
		now.Second())
	return filename.String()
}
func (this *hlsFile) write(data []byte) {
	if this.fileHandler == nil {
		this.fileName = this.generateFileName()
		var err error
		this.fileHandler, err = os.OpenFile(this.fileName, os.O_CREATE|os.O_RDWR, 0777)
		if err != nil {
			fmt.Println("write", err)
			return
		}
	}
	if this.begTime == 0 {
		this.begTime = uint32(time.Now().UnixNano() / 1000 / 1000 / 1000)
	}
	now := uint32(time.Now().UnixNano() / 1000 / 1000 / 1000)

	this.fileHandler.Write(data)
	this.fileSize += uint32(len(data))
	now -= this.begTime
	if now >= TSFILEMAX_DURATION {
		this.fileHandler.Close()
		this.fileHandler = nil
		this.begTime = 0
		this.m3u8.Update(m3u8Tag{duration: now, url: this.fileName})
	}
}

/*
 1. 负责接收原始H264数据
 2. 负责把H264数据转换为ts文件
 3. 负责生成m3u8文件
 4. 支持把相关文件存储在本地磁盘，通过web服务器访问
     也支持直接把相关文件存储NoSql数据库中，目前考虑的数据库：
	 mongdb，Riak
*/
type RawData2Hls struct {
	fileHandler     hlsFile
	muxTsHandle     unsafe.Pointer
	cache           *bytes.Buffer
	frameCount      uint32
	h264FileHandler *os.File
}

func (this *RawData2Hls) Init() {
	this.muxTsHandle = C.createH264MuxTs()
	C.setCB(this.muxTsHandle, unsafe.Pointer(this))
	this.frameCount = 0
	this.fileHandler.Init("shao")
	this.cache = nil
	this.h264FileHandler, _ = os.OpenFile("my.h264", os.O_CREATE|os.O_RDWR, 0777)
}

func (this *RawData2Hls) Uninit() {
	C.releaseH264MuxTs(this.muxTsHandle)
}

/*
func (this *RawData2Hls) goRawH264Data2Ts(frameType uint16, data []byte) {

	if frameType == 1 {
		fmt.Println("***************Recv Mesg ", frameType, len(data))
		this.frameCount++

		if this.frameCount == 1 {

			if this.cache != nil && this.cache.Len() > 0 {
				C.rawH264Data2Ts(this.muxTsHandle,
					unsafe.Pointer(&this.cache.Bytes()[0]),
					C.uint(this.cache.Len()))
				//this.h264FileHandler.Write(this.cache.Bytes())
			}
			C.rawH264Data2Ts(this.muxTsHandle,
				unsafe.Pointer(&data[0]),
				C.uint(len(data)))

		} else if this.frameCount == 2 {
			C.rawH264Data2Ts(this.muxTsHandle,
				unsafe.Pointer(&data[0]),
				C.uint(len(data)))
		}

		if this.frameCount == 3 {
			this.cache = new(bytes.Buffer)
			this.cache.Write(data)
			this.frameCount = 0
		}

	} else {

		this.cache.Write(data)

		//C.rawH264Data2Ts(this.muxTsHandle, unsafe.Pointer(&data[0]), C.uint(len(data)))
		//this.h264FileHandler.Write(data)
	}

}

*/
func (this *RawData2Hls) goRawH264Data2Ts(frameType uint16, data []byte) {

	C.rawH264Data2Ts(this.muxTsHandle, unsafe.Pointer(&data[0]), C.uint(len(data)))
	this.h264FileHandler.Write(data)
	fmt.Println(data[0:4])
}
