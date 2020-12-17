package util

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cavaliercoder/grab"
	"github.com/vbauerster/mpb"
	"github.com/vbauerster/mpb/decor"
)

var (
	// 添加cookie 会增加耗时?
	//rawCookies string = "_gh_sess=FwgO%2Fyn3YGMElYN1Bjespl7Kt7pJkKDzrFBTW5uU5RmKSx%2F4W1bzzeR9aa60vrYQ2W3fOLruqqIrE%2F13cKo%2FtoCF%2F2PyQgKJaxpYfsZ2rBgP%2FCfNHKzH%2BFBTAp2yjTEi8%2BH7MqxcCyEX5esR0IYiKmok7A4OtUZG7GwWgaifIFfcOmqKYLU8azRbGfZialyxx3dVcUv8BzWlrKKt2i1pk133MbmoyVTmRbRGZ5rxFxL6ZWZje8UgtYgZpKqCPoLAFad4L%2F2TOsMmYyfmE7SIJg%3D%3D--zqI1y7son5QZ%2BsD9--GMREzYUyUTsrxw41yerD%2Fg%3D%3D; _octo=GH1.1.821619945.1606290274; logged_in=no; tz=Asia%2FShanghai"
	userAgent string = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/86.0.4240.198 Safari/537.36"
)

type Reader struct {
	io.Reader
	Total   int64
	Current int64
}

//filePart 文件分片
type filePart struct {
	Index int    //文件分片的序号
	From  int64  //开始位置
	To    int64  //结束位置
	Data  []byte //http下载得到的文件内容
	Done  bool   //标注切片是否下载完成
}

//FileDownloader 文件下载器
type FileDownloader struct {
	fileSize       int64
	url            string
	outputFileName string
	totalPart      int //下载线程
	outputDir      string
	doneFilePart   []filePart
}

func (r *Reader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)

	r.Current += int64(n)
	fmt.Printf("\r进度 %.2f%%", float64(r.Current*10000/r.Total)/100)

	return
}

func exists(path string) bool {
	_, err := os.Stat(path) //os.Stat获取文件信息
	if err != nil {
		if os.IsExist(err) {
			return true
		}
		return false
	}
	return true
}

//NewFileDownloader .
func NewFileDownloader(url, outputFileName, outputDir string, totalPart int) *FileDownloader {
	if outputDir == "" {
		wd, err := os.Getwd() //获取当前工作目录
		if err != nil {
			log.Println(err)
		}
		outputDir = wd
	}
	return &FileDownloader{
		fileSize:       0,
		url:            url,
		outputFileName: outputFileName,
		outputDir:      outputDir,
		totalPart:      totalPart,
		doneFilePart:   make([]filePart, totalPart),
	}
}

// getNewRequest 创建一个request
func (d FileDownloader) getNewRequest(method string) (*http.Request, error) {
	r, err := http.NewRequest(
		method,
		d.url,
		nil,
	)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func parseFileInfoFrom(resp *http.Response) string {
	contentDisposition := resp.Header.Get("Content-Disposition")
	if contentDisposition != "" {
		_, params, err := mime.ParseMediaType(contentDisposition)

		if err != nil {
			panic(err)
		}
		return params["filename"]
	}
	filename := filepath.Base(resp.Request.URL.Path)
	return filename
}

//head 获取要下载的文件的基本信息(header) 使用HTTP Method Head
func (d *FileDownloader) head() (int64, error, string) {
	//TODO head request
	r, err := d.getNewRequest("GET") // location地址的签名体包含有GET方法，并不能使用HEAD方法
	r = setNewHeader(r)

	// signatue := getSign(r)
	// values := r.URL.Query()
	// values.Del("X-Amz-Signature")
	// values.Add("X-Amz-Signature", signatue)
	// queryString := values.Encode()
	// regURL, err := regexp.Compile(`\S+\?`)
	// h := regURL.FindString(r.URL.String())
	// r.URL, err = url.Parse(h + queryString)

	if err != nil {
		return 0, err, ""
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return 0, err, ""
	}
	if resp.StatusCode > 299 {
		return 0, errors.New(fmt.Sprintf("Can't process, response is %v", resp.StatusCode)), ""
	}

	length := 0
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		err = errors.New("服务器不支持文件断点续传")
	} else {
		length, err = strconv.Atoi(resp.Header.Get("Content-Length"))
	}

	etag := resp.Header.Get("Etag")

	d.outputFileName = parseFileInfoFrom(resp)

	return int64(length), err, etag
}

//Run 开始下载任务
func (d *FileDownloader) Run() error {
	fileTotalSize, err, etag := d.head()
	if err != nil {
		return err
	}
	d.fileSize = fileTotalSize

	jobs := make([]filePart, d.totalPart)
	eachSize := fileTotalSize / int64(d.totalPart)

	path := filepath.Join(d.outputDir, d.outputFileName+".tmp")

	tmpFile := new(os.File)

	fByte := make([]byte, d.fileSize)

	if exists(path) {
		tmpFile, err = os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		tmpByte, _ := ioutil.ReadAll(tmpFile) //确保fByte len 和 cap 不改变
		for i := range tmpByte {
			fByte[i] = tmpByte[i]
		}
	} else {
		tmpFile, err = os.Create(path)
	}
	if err != nil {
		return err
	}
	defer tmpFile.Close()

	for i := range jobs {
		jobs[i].Index = i
		if i == 0 {
			jobs[i].From = 0
		} else {
			jobs[i].From = jobs[i-1].To + 1
		}
		if i < d.totalPart-1 {
			jobs[i].To = jobs[i].From + eachSize
		} else {
			//the last filePart
			jobs[i].To = fileTotalSize - 1
		}
	}

	for i, j := range jobs {
		tmpJob := j
		emptyTmp := make([]byte, tmpJob.To-j.From)
		if bytes.Compare(emptyTmp, fByte[tmpJob.From:j.To]) != 0 {
			tmpJob.Data = fByte[j.From : j.To+1]
			tmpJob.Done = true
			d.doneFilePart[tmpJob.Index] = tmpJob
		} else {
			tmpJob.Done = false
		}
		jobs[i] = tmpJob
	}

	doneWg := new(sync.WaitGroup)
	p := mpb.New(mpb.WithWaitGroup(doneWg))
	numBars := 10
	var wg sync.WaitGroup

	var bars []*mpb.Bar
	for i := 0; i < numBars; i++ {
		if !jobs[i].Done {
			total := jobs[i].To - jobs[i].From
			wg.Add(1)
			task := fmt.Sprintf("Task#%02d:", i)
			job := "downloading"
			b := p.AddBar(total,
				mpb.PrependDecorators(
					decor.Name(task, decor.WC{W: len(task) + 1, C: decor.DidentRight}),
					decor.OnComplete(decor.Name(job, decor.WCSyncSpaceR), "done!"),
					decor.CountersKibiByte("% .2f / % .2f"),
				),
				mpb.AppendDecorators(
					decor.EwmaETA(decor.ET_STYLE_GO, 90),
					decor.Name(" ] "),
					decor.EwmaSpeed(decor.UnitKiB, "% .1f", 60),
					decor.OnComplete(decor.Percentage(decor.WC{W: 5}), ""),
				),
			)
			go func(job filePart, b *mpb.Bar) {
				defer wg.Done()
				err := d.downloadPart(job, tmpFile, b)
				if err != nil {
					log.Println("下载文件失败:", err, job)
				}
			}(jobs[i], b)
			bars = append(bars, b)
		}
	}

	wg.Wait()
	p.Wait()
	return d.checkIntegrity(etag, tmpFile)
}

//下载分片
func (d FileDownloader) downloadPart(c filePart, f *os.File, b *mpb.Bar) error {
	r, err := d.getNewRequest("GET")
	r = setNewHeader(r)
	if err != nil {
		return err
	}
	r.Header.Set("Range", fmt.Sprintf("bytes=%v-%v", c.From, c.To))
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return err
	}
	if resp.StatusCode > 299 {
		return errors.New(fmt.Sprintf("服务器错误状态码: %v", resp.StatusCode))
	}
	defer resp.Body.Close()
	proxyReader := b.ProxyReader(resp.Body)
	defer proxyReader.Close()

	bs, err := ioutil.ReadAll(proxyReader)
	if err != nil {
		return err
	}
	if len(bs) != int(c.To-c.From+1) {
		c.Data = bs
		c.Done = false
	}
	c.Data = bs
	c.Done = true

	d.doneFilePart[c.Index] = c

	_, err = f.WriteAt(bs, int64(c.From))

	if err != nil {
		c.Done = true
	}

	return err
}

//checkIntegrity 合并下载的文件
func (d FileDownloader) checkIntegrity(etag string, t *os.File) error {
	defer t.Close()
	hash := md5.New()
	totalSize := 0

	for _, s := range d.doneFilePart {
		hash.Write(s.Data)
		totalSize += len(s.Data)
	}

	if int64(totalSize) != d.fileSize {
		return errors.New("文件不完整")
	}

	if hex.EncodeToString(hash.Sum(nil)) != etag[1:len(etag)-1] {
		return errors.New("文件损坏")
	} else {
		log.Println("文件md5校验成功")
	}

	os.Rename(filepath.Join(d.outputDir, d.outputFileName+".tmp"), filepath.Join(d.outputDir, d.outputFileName))

	return nil
}

func getRedirectInfo(u, rawCookies, userAgent string) (*http.Response, error) {
	var a *url.URL
	a, _ = url.Parse(u)
	header := http.Header{}

	//header.Add("Cookie", rawCookies)
	header.Add("User-Agent", userAgent)
	request := http.Request{
		Header: header,
		Method: "GET",
		URL:    a,
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(&request)
	if err != nil {
		return nil, err
	}

	return response, nil
}

func setNewHeader(r *http.Request) *http.Request {
	r.Header.Add("User-Agent", userAgent)
	r.Header.Add("Upgrade-Insecure-Requests", "1")
	return r
}

// http://cavaliercoder.com/blog/downloading-large-files-in-go.html
// https://github.com/cavaliercoder/grab
func GrabFile(url string, dir string) error {
	client := grab.NewClient()
	req, _ := grab.NewRequest(dir, url)

	// start download
	fmt.Printf("Downloading %v...\n", req.URL())
	resp := client.Do(req)
	fmt.Printf("  %v\n", resp.HTTPResponse.Status)

	// start UI loop
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()

Loop:
	for {
		select {
		case <-t.C:
			fmt.Printf("  transferred %v / %v bytes (%.2f%%)\n",
				resp.BytesComplete(),
				resp.Size,
				100*resp.Progress())

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}

	// check for errors
	if err := resp.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		return err
	}

	fmt.Printf("Download saved to ./%v \n", resp.Filename)
	return nil
}

func DownloadFile(URL string, dir string) error {
	start := time.Now()
	a, err := getRedirectInfo(URL, "", userAgent)
	if err != nil {
		return err
	}
	location := a.Header.Get("Location")
	if location == "" {
		location = URL
	} else {
		fmt.Printf("重定向完成耗时: %f second\n", time.Now().Sub(start).Seconds())
	}
	if !strings.HasPrefix(URL, "https://github") {
		return GrabFile(location, dir)
	}

	startTime := time.Now()
	downloader := NewFileDownloader(location, "", dir, 10)
	if err := downloader.Run(); err != nil {
		if err.Error() != "服务器不支持文件断点续传" {
			log.Println(err)
			return err
		} else {
			return GrabFile(location, dir)
		}
	}
	fmt.Printf("\n 文件下载完成耗时: %f second\n", time.Now().Sub(startTime).Seconds())
	return nil
}
