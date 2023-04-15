package gdl

import (
    "io"
)

type OffsetWriter struct {
    io.WriterAt
    offset int64
}

func (dst *OffsetWriter) Write(b []byte) (n int, err error) {
    n, err = dst.WriteAt(b, dst.offset)
    dst.offset += int64(n)
    return
}

type Chunk struct {
    Start, End uint64
}
