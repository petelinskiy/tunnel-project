package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

// Obfuscator обфусцирует данные
type Obfuscator struct {
	key []byte
}

// NewObfuscator создает новый обфускатор
func NewObfuscator(key []byte) (*Obfuscator, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes")
	}
	return &Obfuscator{key: key}, nil
}

// Obfuscate обфусцирует данные с использованием AES-GCM
func (o *Obfuscator) Obfuscate(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(o.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Deobfuscate деобфусцирует данные
func (o *Obfuscator) Deobfuscate(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(o.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// AddPadding добавляет случайный padding для маскировки размера
func AddPadding(data []byte, targetSize int) []byte {
	if len(data) >= targetSize {
		return data
	}
	
	padded := make([]byte, targetSize)
	copy(padded, data)
	
	// Заполняем остаток случайными данными
	_, _ = rand.Read(padded[len(data):])
	
	return padded
}

// RemovePadding удаляет padding (требует передачи оригинального размера)
func RemovePadding(data []byte, originalSize int) []byte {
	if originalSize > len(data) {
		return data
	}
	return data[:originalSize]
}

// XORObfuscate простая XOR обфускация (быстрая, для дополнительного слоя)
func XORObfuscate(data []byte, key []byte) []byte {
	result := make([]byte, len(data))
	for i := 0; i < len(data); i++ {
		result[i] = data[i] ^ key[i%len(key)]
	}
	return result
}
