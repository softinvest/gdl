package gdl

import (
    "context"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "runtime"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "time"
)

type (
    Info struct {
        Size      uint64
        Rangeable bool
    }

    Download struct {
        Client *http.Client
        Concurrency uint
        URL, Dir, Dest string
        Interval, ChunkSize, MinChunkSize, MaxChunkSize uint64
        Header []GdlHeader
        path string
        unsafeName string
        ctx context.Context
        size, lastSize uint64
        info *Info
        chunks []*Chunk
        startedAt time.Time
    }

    GdlHeader struct {
        Key   string
        Value string
    }
)

func (d *Download) GetInfoOrDownload() (*Info, error) {
    var (
        err  error
        dest *os.File
        req  *http.Request
        res  *http.Response
    )

    if req, err = NewRequest(d.ctx, "GET", d.URL, append(d.Header, GdlHeader{"Range", "bytes=0-0"})); err != nil {
        return &Info{}, err
    }

    if res, err = d.Client.Do(req); err != nil {
        return &Info{}, err
    }
    defer res.Body.Close()

    if res.StatusCode >= 300 {
        return &Info{}, fmt.Errorf("Response status fail: %d", res.StatusCode)
    }

    d.unsafeName = res.Header.Get("content-disposition")

    if dest, err = os.Create(d.Path()); err != nil {
        return &Info{}, err
    }
    defer dest.Close()

    if _, err = io.Copy(dest, io.TeeReader(res.Body, d)); err != nil {
        return &Info{}, err
    }

    if cr := res.Header.Get("content-range"); cr != "" && res.ContentLength == 1 {
        l := strings.Split(cr, "/")
        if len(l) == 2 {
            if length, err := strconv.ParseUint(l[1], 10, 64); err == nil {
                return &Info{
                    Size:      length,
                    Rangeable: true,
                }, nil
            }
        }
        return &Info{}, fmt.Errorf("Invalid content-range header in response: %s", cr)
    }

    return &Info{}, nil
}

func (d *Download) Init() (err error) {
    d.startedAt = time.Now()
    if d.Client == nil {
        d.Client = DefaultClient
    }

    if d.ctx == nil {
        d.ctx = context.Background()
    }

    if d.info, err = d.GetInfoOrDownload(); err != nil {
        return err
    }

    if d.info.Rangeable == false {
        return nil
    }

    if d.Concurrency == 0 {
        d.Concurrency = getDefaultConcurrency()
    }

    if d.ChunkSize == 0 {
        d.ChunkSize = getDefaultChunkSize(d.info.Size, d.MinChunkSize, d.MaxChunkSize, uint64(d.Concurrency))
    }

    chunksLen := d.info.Size / d.ChunkSize
    d.chunks = make([]*Chunk, 0, chunksLen)

    for i := uint64(0); i < chunksLen; i++ {
        chunk := new(Chunk)
        d.chunks = append(d.chunks, chunk)
        chunk.Start = (d.ChunkSize * i) + i
        chunk.End = chunk.Start + d.ChunkSize
        if chunk.End >= d.info.Size || i == chunksLen-1 {
            chunk.End = d.info.Size - 1
            break
        }
    }

    return nil
}

func (d *Download) Start() (err error) {
    if d.info.Rangeable == false {
        select {
        case <-d.ctx.Done():
            return d.ctx.Err()
        default:
            return nil
        }
    }

    file, err := os.Create(d.Path())
    if err != nil {
        return err
    }

    defer file.Close()

    file.Truncate(int64(d.TotalSize()))
    errs := make(chan error, 1)
    go d.dl(file, errs)
    select {
        case err = <-errs:
        case <-d.ctx.Done():
            err = d.ctx.Err()
    }

    return
}

func (d *Download) Context() context.Context {
    return d.ctx
}

func (d *Download) TotalSize() uint64 {
    return d.info.Size
}

func (d *Download) Size() uint64 {
    return atomic.LoadUint64(&d.size)
}

func (d *Download) Write(b []byte) (int, error) {
    n := len(b)
    atomic.AddUint64(&d.size, uint64(n))
    return n, nil
}

func (d *Download) dl(dest io.WriterAt, errC chan error) {
    var (
        wg sync.WaitGroup
        max = make(chan int, d.Concurrency)
    )

    for i := 0; i < len(d.chunks); i++ {
        max <- 1
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            if err := d.DownloadChunk(d.chunks[i], &OffsetWriter{dest, int64(d.chunks[i].Start)}); err != nil {
                errC <- err
                return
            }

            <-max
        }(i)
    }

    wg.Wait()
    errC <- nil
}

func (d *Download) Path() string {
    if d.path == "" {
        d.path = GetFilename(d.URL)
        if d.Dest != "" {
            d.path = d.Dest
        } else if d.unsafeName != "" {
            if path := getNameFromHeader(d.unsafeName); path != "" {
                d.path = path
            }
        }
        d.path = filepath.Join(d.Dir, d.path)
    }

    return d.path
}

func (d *Download) DownloadChunk(c *Chunk, dest io.Writer) error {
    var (
        err error
        req *http.Request
        res *http.Response
    )
    if req, err = NewRequest(d.ctx, "GET", d.URL, d.Header); err != nil {
        return err
    }
    contentRange := fmt.Sprintf("bytes=%d-%d", c.Start, c.End)
    req.Header.Set("Range", contentRange)
    if res, err = d.Client.Do(req); err != nil {
        return err
    }
    if res.ContentLength != int64(c.End-c.Start+1) {
        return fmt.Errorf(
            "Range request returned invalid Content-Length: %d however the range was: %s",
            res.ContentLength, contentRange,
        )
    }
    defer res.Body.Close()
    _, err = io.CopyN(dest, io.TeeReader(res.Body, d), res.ContentLength)

    return err
}

func getDefaultConcurrency() uint {
    c := uint(runtime.NumCPU() * 3)
    if c > 20 {
        c = 20
    }
    if c <= 2 {
        c = 4
    }

    return c
}

func getDefaultChunkSize(totalSize, min, max, concurrency uint64) uint64 {
    cs := totalSize / concurrency
    if cs >= 102400000 {
        cs = cs / 2
    }
    if min == 0 {
        min = 2097152
        if min >= totalSize {
            min = totalSize / 2
        }
    }

    if cs < min {
        cs = min
    }

    if max > 0 && cs > max {
        cs = max
    }

    if cs >= totalSize {
        cs = totalSize / 2
    }

    return cs
}
