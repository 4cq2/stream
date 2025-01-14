package widevine

import (
   "41.neocities.org/protobuf"
   "crypto"
   "crypto/aes"
   "crypto/cipher"
   "crypto/rsa"
   "crypto/sha1"
   "crypto/x509"
   "encoding/pem"
   "github.com/chmike/cmac-go"
)

func unpad(b []byte) []byte {
   if len(b) >= 1 {
      pad := b[len(b)-1]
      if len(b) >= int(pad) {
         b = b[:len(b)-int(pad)]
      }
   }
   return b
}

type Cdm struct {
   license_request []byte
   private_key *rsa.PrivateKey
}

func (c *Cdm) Block(body ResponseBody) (cipher.Block, error) {
   session_key, err := rsa.DecryptOAEP(
      sha1.New(), nil, c.private_key, body.session_key(), nil,
   )
   if err != nil {
      return nil, err
   }
   hash, err := cmac.New(aes.NewCipher, session_key)
   if err != nil {
      return nil, err
   }
   var data []byte
   data = append(data, 1)
   data = append(data, "ENCRYPTION"...)
   data = append(data, 0)
   data = append(data, c.license_request...)
   data = append(data, 0, 0, 0, 128) // hash.Size()
   _, err = hash.Write(data)
   if err != nil {
      return nil, err
   }
   return aes.NewCipher(hash.Sum(nil))
}

func (c *Cdm) New(private_key, client_id, pssh []byte) error {
   block, _ := pem.Decode(private_key)
   var err error
   c.private_key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
   if err != nil {
      // L1
      key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
      if err != nil {
         return err
      }
      c.private_key = key.(*rsa.PrivateKey)
   }
   c.license_request = protobuf.Message{ // LicenseRequest
      1: {protobuf.Bytes(client_id)}, // ClientIdentification client_id
      2: {protobuf.Message{ // ContentIdentification content_id
         1: {protobuf.Message{ // WidevinePsshData widevine_pssh_data
            1: {protobuf.Bytes(pssh)},
         }},
      }},
   }.Marshal()
   return nil
}

func (c *Cdm) RequestBody() ([]byte, error) {
   hash := sha1.Sum(c.license_request)
   signature, err := rsa.SignPSS(
      rand{},
      c.private_key,
      crypto.SHA1,
      hash[:],
      &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash},
   )
   if err != nil {
      return nil, err
   }
   // SignedMessage
   signed := protobuf.Message{}
   // kktv.me
   // type: LICENSE_REQUEST
   signed.AddVarint(1, 1)
   // LicenseRequest msg
   signed.AddBytes(2, c.license_request)
   // bytes signature
   signed.AddBytes(3, signature)
   return signed.Marshal(), nil
}

type KeyContainer struct {
   Message protobuf.Message
}

func (k KeyContainer) Id() []byte {
   value, _ := k.Message.GetBytes(1)()
   return value
}

func (k KeyContainer) iv() []byte {
   value, _ := k.Message.GetBytes(2)()
   return value
}

func (k KeyContainer) Key(block cipher.Block) []byte {
   key, _ := k.Message.GetBytes(3)()
   cipher.NewCBCDecrypter(block, k.iv()).CryptBlocks(key, key)
   return unpad(key)
}

func (k KeyContainer) Type() uint64 {
   value, _ := k.Message.GetVarint(4)()
   return uint64(value)
}

func (k KeyContainer) SecurityLevel() uint64 {
   value, _ := k.Message.GetVarint(5)()
   return uint64(value)
}

func (k KeyContainer) TrackLabel() string {
   value, _ := k.Message.GetBytes(12)()
   return string(value)
}

type PsshData struct {
   ContentId []byte
   KeyIds [][]byte
}

func (p *PsshData) Marshal() []byte {
   message := protobuf.Message{}
   for _, key_id := range p.KeyIds {
      message.AddBytes(2, key_id)
   }
   if len(p.ContentId) >= 1 {
      message.AddBytes(4, p.ContentId)
   }
   return message.Marshal()
}

func (r ResponseBody) Container() func() (KeyContainer, bool) {
   value, _ := r.message.Get(2)()
   values := value.Get(3)
   return func() (KeyContainer, bool) {
      value, ok := values()
      return KeyContainer{value}, ok
   }
}

func (r *ResponseBody) Unmarshal(data []byte) error {
   r.message = protobuf.Message{}
   return r.message.Unmarshal(data)
}

type ResponseBody struct {
   message protobuf.Message
}

func (r ResponseBody) session_key() []byte {
   value, _ := r.message.GetBytes(4)()
   return value
}

type Wrapper interface {
   Wrap([]byte) ([]byte, error)
}

type rand struct{}

func (rand) Read(b []byte) (int, error) {
   return len(b), nil
}
