// Package storage 管理 Blob 和 Delta 文件的读写。
package storage

// 魔数和常量（跨文件共享）
const (
	// Blob 格式
	BlobMagic      = uint32(0x424C0001) // "BL" + v1
	BlobHeaderSize = 6                  // 4B magic + 2B compress_method

	// Delta 格式
	DeltaMagic      = uint32(0x43440001) // "CD" + v1
	DeltaHeaderSize = 14                 // 4B magic + 4B version_id + 2B compress_method + 4B delta_length
	// 注意：meta_length (4B) 不在 header 中，紧跟 diff 内容之后，见 delta.go

	// 压缩方法
	CompressNone = uint16(0x0000)
	CompressZstd = uint16(0x0001)
)
