package password

import "testing"

func TestHashAndVerify(t *testing.T) {
	const plain = "Correct-Horse-9!"

	encoded, err := Hash(plain)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if encoded == plain {
		t.Fatal("Hash() returned the plaintext")
	}
	if !Verify(plain, encoded) {
		t.Fatal("Verify() rejected the correct password")
	}
	if Verify("wrong-password", encoded) {
		t.Fatal("Verify() accepted a wrong password")
	}
	if Verify(plain, "not-a-hash") {
		t.Fatal("Verify() accepted a malformed hash")
	}
}

func TestHashUsesFreshSalt(t *testing.T) {
	first, err := Hash("Same-Password-1!")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	second, err := Hash("Same-Password-1!")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if first == second {
		t.Fatal("two hashes of the same password are identical")
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		username string
		wantErr  bool
	}{
		{name: "valid", password: "Str0ng-Pass!", username: "admin"},
		{name: "too short", password: "Aa1!", username: "admin", wantErr: true},
		{name: "too long", password: "Aa1!" + string(make([]byte, 200)), username: "admin", wantErr: true},
		{name: "no uppercase", password: "str0ng-pass!", username: "admin", wantErr: true},
		{name: "no lowercase", password: "STR0NG-PASS!", username: "admin", wantErr: true},
		{name: "no digit", password: "Strong-Pass!", username: "admin", wantErr: true},
		{name: "no symbol", password: "Str0ngPass1", username: "admin", wantErr: true},
		{name: "equals username", password: "Adminnnn1!", username: "adminnnn1!", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidatePassword(tt.password, tt.username); (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePassword() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateUsername(t *testing.T) {
	valid := []string{"admin", "ops.team", "user_1", "a-b-c"}
	for _, username := range valid {
		if err := ValidateUsername(username); err != nil {
			t.Errorf("ValidateUsername(%q) = %v", username, err)
		}
	}
	invalid := []string{"", "ab", "Admin", "with space", "semi;colon", "ümlaut"}
	for _, username := range invalid {
		if err := ValidateUsername(username); err == nil {
			t.Errorf("ValidateUsername(%q) accepted an invalid username", username)
		}
	}
}
