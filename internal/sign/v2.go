package sign

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"os"
)

const (
	v2BlockID        = 0x7109871A
	rsaPKCS1SHA256   = 0x0103
	contentChunkSize = 1024 * 1024
)

var apkSigBlockMagic = []byte("APK Sig Block 42")

type eocdInfo struct {
	pos      int
	cdOffset uint32
	cdSize   uint32
}

func SignFile(inPath, outPath string, key *Key) error {
	data, err := os.ReadFile(inPath)
	if err != nil {
		return err
	}
	signed, err := Sign(data, key)
	if err != nil {
		return err
	}
	if _, err := Verify(signed); err != nil {
		return fmt.Errorf("签名后自校验失败: %w", err)
	}
	return os.WriteFile(outPath, signed, 0o644)
}

func Sign(data []byte, key *Key) ([]byte, error) {
	return signWithKeys(data, []*Key{key})
}

func signWithKeys(data []byte, keys []*Key) ([]byte, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("至少需要一个签名密钥")
	}
	eocd, err := findEOCD(data)
	if err != nil {
		return nil, err
	}
	if uint64(eocd.cdOffset)+uint64(eocd.cdSize) != uint64(eocd.pos) {
		return nil, fmt.Errorf("不支持的 APK 布局：Central Directory 与 EOCD 之间有空隙，可能已签名")
	}
	if int(eocd.cdOffset) >= 16 && bytes.Equal(data[int(eocd.cdOffset)-16:int(eocd.cdOffset)], apkSigBlockMagic) {
		return nil, fmt.Errorf("输入 APK 已包含签名块")
	}

	before := data[:eocd.cdOffset]
	central := data[int(eocd.cdOffset) : int(eocd.cdOffset)+int(eocd.cdSize)]
	eocdBytes := data[eocd.pos:]
	digest := computeContentDigest([][]byte{before, central, eocdBytes})

	var signersRaw []byte
	for _, key := range keys {
		signer, err := buildSigner(key, digest)
		if err != nil {
			return nil, err
		}
		signersRaw = append(signersRaw, lp(signer)...)
	}
	value := lp(signersRaw)

	pairLen := uint64(4 + len(value))
	var pair bytes.Buffer
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], pairLen)
	pair.Write(tmp[:])
	var idBuf [4]byte
	binary.LittleEndian.PutUint32(idBuf[:], v2BlockID)
	pair.Write(idBuf[:])
	pair.Write(value)

	var block bytes.Buffer
	blockSize := uint64(pair.Len() + 8 + 16)
	binary.LittleEndian.PutUint64(tmp[:], blockSize)
	block.Write(tmp[:])
	block.Write(pair.Bytes())
	binary.LittleEndian.PutUint64(tmp[:], blockSize)
	block.Write(tmp[:])
	block.Write(apkSigBlockMagic)

	newCDOffset := uint64(eocd.cdOffset) + uint64(block.Len())
	if newCDOffset > 0xFFFFFFFF {
		return nil, fmt.Errorf("APK 太大，签名后超过 4GB")
	}

	newEOCD := append([]byte(nil), eocdBytes...)
	binary.LittleEndian.PutUint32(newEOCD[16:], uint32(newCDOffset))

	out := make([]byte, 0, len(data)+block.Len())
	out = append(out, data[:eocd.cdOffset]...)
	out = append(out, block.Bytes()...)
	out = append(out, central...)
	out = append(out, newEOCD...)
	return out, nil
}

func buildSigner(key *Key, digest []byte) ([]byte, error) {
	digestPair := append(u32(rsaPKCS1SHA256), lp(digest)...)
	digestsRaw := lp(digestPair)
	certsRaw := lp(key.CertDER)
	attrsRaw := []byte{}
	signedData := lp(digestsRaw)
	signedData = append(signedData, lp(certsRaw)...)
	signedData = append(signedData, lp(attrsRaw)...)

	hashed := sha256.Sum256(signedData)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key.Private, crypto.SHA256, hashed[:])
	if err != nil {
		return nil, err
	}
	sigPair := append(u32(rsaPKCS1SHA256), lp(sig)...)
	sigsRaw := lp(sigPair)

	pubDER, err := x509.MarshalPKIXPublicKey(&key.Private.PublicKey)
	if err != nil {
		return nil, err
	}

	signer := lp(signedData)
	signer = append(signer, lp(sigsRaw)...)
	signer = append(signer, lp(pubDER)...)
	return signer, nil
}

func computeContentDigest(sections [][]byte) []byte {
	totalChunks := 0
	for _, s := range sections {
		totalChunks += (len(s) + contentChunkSize - 1) / contentChunkSize
	}
	final := sha256.New()
	final.Write([]byte{0x5A})
	var count [4]byte
	binary.LittleEndian.PutUint32(count[:], uint32(totalChunks))
	final.Write(count[:])
	for _, s := range sections {
		for off := 0; off < len(s); off += contentChunkSize {
			end := off + contentChunkSize
			if end > len(s) {
				end = len(s)
			}
			chunk := s[off:end]
			h := sha256.New()
			h.Write([]byte{0xA5})
			var n [4]byte
			binary.LittleEndian.PutUint32(n[:], uint32(len(chunk)))
			h.Write(n[:])
			h.Write(chunk)
			final.Write(h.Sum(nil))
		}
	}
	return final.Sum(nil)
}

type VerifyResult struct {
	Certificate    *x509.Certificate
	SignatureAlgID uint32
	ContentDigest  []byte
}

func Verify(data []byte) (*VerifyResult, error) {
	eocd, err := findEOCD(data)
	if err != nil {
		return nil, err
	}
	cdEnd := int(eocd.cdOffset)
	if cdEnd < 32 || !bytes.Equal(data[cdEnd-16:cdEnd], apkSigBlockMagic) {
		return nil, fmt.Errorf("未找到 APK Signing Block（APK 未签名）")
	}
	blockStart, err := findSigningBlockStart(data, cdEnd)
	if err != nil {
		return nil, err
	}
	if cdEnd-blockStart < 32 {
		return nil, fmt.Errorf("签名块过小")
	}
	sizeLE := binary.LittleEndian.Uint64(data[blockStart:])
	if binary.LittleEndian.Uint64(data[cdEnd-24:]) != sizeLE {
		return nil, fmt.Errorf("签名块首尾长度不一致")
	}

	var v2Value []byte
	pos := blockStart + 8
	end := cdEnd - 24
	for pos < end {
		if pos+8 > end {
			return nil, fmt.Errorf("签名块格式损坏")
		}
		pairLen := binary.LittleEndian.Uint64(data[pos:])
		if pairLen < 4 || uint64(pos)+8+pairLen > uint64(end) {
			return nil, fmt.Errorf("签名块 pair 长度非法")
		}
		id := binary.LittleEndian.Uint32(data[pos+8:])
		value := data[pos+12 : pos+8+int(pairLen)]
		if id == v2BlockID {
			v2Value = value
		}
		pos += 8 + int(pairLen)
	}
	if v2Value == nil {
		return nil, fmt.Errorf("未找到 v2 签名块")
	}

	p := &parser{data: v2Value}
	signersRaw, err := p.lp()
	if err != nil {
		return nil, fmt.Errorf("解析 signers 失败: %w", err)
	}
	sp := &parser{data: signersRaw}
	var result *VerifyResult
	signers := 0
	for sp.remaining() > 0 {
		signerBytes, err := sp.lp()
		if err != nil {
			return nil, fmt.Errorf("解析 signer 失败: %w", err)
		}
		signers++
		res, err := verifySigner(data, eocd, blockStart, signerBytes)
		if err != nil {
			return nil, err
		}
		if result == nil {
			result = res
		}
	}
	if signers == 0 {
		return nil, fmt.Errorf("签名块中没有 signer")
	}
	return result, nil
}

func verifySigner(data []byte, eocd *eocdInfo, blockStart int, signerBytes []byte) (*VerifyResult, error) {
	signer := &parser{data: signerBytes}
	signedData, err := signer.lp()
	if err != nil {
		return nil, fmt.Errorf("解析 signedData 失败: %w", err)
	}
	sigsRaw, err := signer.lp()
	if err != nil {
		return nil, fmt.Errorf("解析 signatures 失败: %w", err)
	}
	pubDER, err := signer.lp()
	if err != nil {
		return nil, fmt.Errorf("解析 publicKey 失败: %w", err)
	}

	pubAny, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		return nil, fmt.Errorf("解析公钥失败: %w", err)
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("仅支持 RSA 签名")
	}

	hashed := sha256.Sum256(signedData)
	var verified bool
	sigParser := &parser{data: sigsRaw}
	for sigParser.remaining() > 0 {
		sigEntry, err := sigParser.lp()
		if err != nil {
			return nil, fmt.Errorf("解析 signature 失败: %w", err)
		}
		ep := &parser{data: sigEntry}
		alg, err := ep.u32()
		if err != nil {
			return nil, err
		}
		sig, err := ep.lp()
		if err != nil {
			return nil, err
		}
		if alg != rsaPKCS1SHA256 {
			continue
		}
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, hashed[:], sig); err != nil {
			return nil, fmt.Errorf("签名校验失败: %w", err)
		}
		verified = true
	}
	if !verified {
		return nil, fmt.Errorf("没有可用的 RSA/SHA-256 签名")
	}

	sd := &parser{data: signedData}
	digestsRaw, err := sd.lp()
	if err != nil {
		return nil, fmt.Errorf("解析 digests 失败: %w", err)
	}
	certsRaw, err := sd.lp()
	if err != nil {
		return nil, fmt.Errorf("解析 certificates 失败: %w", err)
	}
	if _, err := sd.lp(); err != nil {
		return nil, fmt.Errorf("解析 additionalAttributes 失败: %w", err)
	}

	dp := &parser{data: digestsRaw}
	var contentDigest []byte
	for dp.remaining() > 0 {
		digestEntry, err := dp.lp()
		if err != nil {
			return nil, err
		}
		ep := &parser{data: digestEntry}
		alg, err := ep.u32()
		if err != nil {
			return nil, err
		}
		d, err := ep.lp()
		if err != nil {
			return nil, err
		}
		if alg == rsaPKCS1SHA256 {
			contentDigest = d
		}
	}
	if contentDigest == nil {
		return nil, fmt.Errorf("缺少 RSA/SHA-256 内容摘要")
	}

	cp := &parser{data: certsRaw}
	certDER, err := cp.lp()
	if err != nil {
		return nil, fmt.Errorf("解析证书失败: %w", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}
	certPub, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(certPub, pubDER) {
		return nil, fmt.Errorf("证书公钥与签名公钥不一致")
	}

	central := data[int(eocd.cdOffset) : int(eocd.cdOffset)+int(eocd.cdSize)]
	eocdBytes := append([]byte(nil), data[eocd.pos:]...)
	binary.LittleEndian.PutUint32(eocdBytes[16:], uint32(blockStart))
	actual := computeContentDigest([][]byte{data[:blockStart], central, eocdBytes})
	if !bytes.Equal(actual, contentDigest) {
		return nil, fmt.Errorf("APK 内容摘要不匹配（文件被篡改）")
	}

	return &VerifyResult{
		Certificate:    cert,
		SignatureAlgID: rsaPKCS1SHA256,
		ContentDigest:  contentDigest,
	}, nil
}

func findEOCD(data []byte) (*eocdInfo, error) {
	if len(data) < 22 {
		return nil, fmt.Errorf("文件太小，不是有效 ZIP")
	}
	start := len(data) - 22 - 0xFFFF
	if start < 0 {
		start = 0
	}
	for i := len(data) - 22; i >= start; i-- {
		if binary.LittleEndian.Uint32(data[i:]) != 0x06054B50 {
			continue
		}
		commentLen := int(binary.LittleEndian.Uint16(data[i+20:]))
		if i+22+commentLen != len(data) {
			continue
		}
		cdSize := binary.LittleEndian.Uint32(data[i+12:])
		cdOffset := binary.LittleEndian.Uint32(data[i+16:])
		if cdSize == 0xFFFFFFFF || cdOffset == 0xFFFFFFFF {
			return nil, fmt.Errorf("暂不支持 ZIP64 APK")
		}
		if uint64(cdOffset)+uint64(cdSize) > uint64(i) {
			return nil, fmt.Errorf("Central Directory 偏移非法")
		}
		return &eocdInfo{pos: i, cdOffset: cdOffset, cdSize: cdSize}, nil
	}
	return nil, fmt.Errorf("未找到 EOCD")
}

func findSigningBlockStart(data []byte, cdOffset int) (int, error) {
	if cdOffset < 32 || cdOffset > len(data) {
		return 0, fmt.Errorf("未找到 APK Signing Block（APK 未签名）")
	}
	if !bytes.Equal(data[cdOffset-16:cdOffset], apkSigBlockMagic) {
		return 0, fmt.Errorf("未找到 APK Signing Block（APK 未签名）")
	}
	size := binary.LittleEndian.Uint64(data[cdOffset-24:])
	if size < 24 || size > uint64(cdOffset-8) {
		return 0, fmt.Errorf("签名块长度非法")
	}
	start := cdOffset - 8 - int(size)
	if binary.LittleEndian.Uint64(data[start:]) != size {
		return 0, fmt.Errorf("签名块首尾长度不一致")
	}
	return start, nil
}

type parser struct {
	data []byte
	off  int
}

func (p *parser) remaining() int { return len(p.data) - p.off }

func (p *parser) u32() (uint32, error) {
	if p.remaining() < 4 {
		return 0, fmt.Errorf("数据截断")
	}
	v := binary.LittleEndian.Uint32(p.data[p.off:])
	p.off += 4
	return v, nil
}

func (p *parser) lp() ([]byte, error) {
	n, err := p.u32()
	if err != nil {
		return nil, err
	}
	if uint64(n) > uint64(p.remaining()) {
		return nil, fmt.Errorf("长度前缀越界")
	}
	out := p.data[p.off : p.off+int(n)]
	p.off += int(n)
	return out, nil
}

func u32(v uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return b[:]
}

func lp(b []byte) []byte {
	if len(b) > 0xFFFFFFFF {
		panic("length prefix too large")
	}
	out := make([]byte, 4+len(b))
	binary.LittleEndian.PutUint32(out, uint32(len(b)))
	copy(out[4:], b)
	return out
}
