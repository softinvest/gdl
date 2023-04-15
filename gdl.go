package gdl

import (
    "context"
    "errors"
    "net/http"
    "time"
)

type Gdl struct {
    Client *http.Client
    ctx context.Context
}

var UserAgent = "GDL/1.0"

var ErrDownloadAborted = errors.New("Operation aborted")

var DefaultClient = &http.Client{
    Transport: &http.Transport{
        MaxIdleConns:        10,
        IdleConnTimeout:     30 * time.Second,
        TLSHandshakeTimeout: 5 * time.Second,
        Proxy:               http.ProxyFromEnvironment,
    },
}

func (g Gdl) Download(URL, dest string) error {
    return g.Do(&Download{
        ctx:    g.ctx,
        URL:    URL,
        Dest:   dest,
        Client: g.Client,
    })
}

func (g Gdl) Do(dl *Download) error {
    if err := dl.Init(); err != nil {
        return err
    }

    return dl.Start()
}

func New() *Gdl {
    return NewWithContext(context.Background())
}

func Fetch(url string, dir string, dest string, chunksize uint64) (err error) {
    var g *Gdl = New()
    return g.Do(&Download{
        URL:         url,
        Dir:         dir,
        Dest:        dest,
        ChunkSize:   chunksize,
    })
}

func NewWithContext(ctx context.Context) *Gdl {
    return &Gdl{
        ctx:    ctx,
        Client: DefaultClient,
    }
}

func NewRequest(ctx context.Context, method, URL string, header []GdlHeader) (req *http.Request, err error) {
    if req, err = http.NewRequestWithContext(ctx, method, URL, nil); err != nil {
        return
    }
    req.Header.Set("User-Agent", UserAgent)
    for _, h := range header {
        req.Header.Set(h.Key, h.Value)
    }

    return
}
