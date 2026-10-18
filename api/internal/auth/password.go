package auth

import "golang.org/x/crypto/bcrypt"

// HashPassword bcrypt-hashes a password for storage in users.password_hash.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// dummyHash is compared against on logins for unknown emails so they take as
// long as a wrong password and don't reveal which accounts exist.
var dummyHash, _ = HashPassword("timing-equalizer")

func checkPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
