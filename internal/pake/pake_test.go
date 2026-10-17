package pake

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// RFC 9382 Appendix B vectors: A, B, w, x, y, pA, pB, TT, Ke, A conf, B conf.
var vectors = [][11]string{
	{"server", "client", "2ee57912099d31560b3a44b1184b9b4866e904c49d12ac5042c97dca461b1a5f", "43dd0fd7215bdcb482879fca3220c6a968e66d70b1356cac18bb26c84a78d729", "dcb60106f276b02606d8ef0a328c02e4b629f84f89786af5befb0bc75b6e66be", "04a56fa807caaa53a4d28dbb9853b9815c61a411118a6fe516a8798434751470f9010153ac33d0d5f2047ffdb1a3e42c9b4e6be662766e1eeb4116988ede5f912c", "0406557e482bd03097ad0cbaa5df82115460d951e3451962f1eaf4367a420676d09857ccbc522686c83d1852abfa8ed6e4a1155cf8f1543ceca528afb591a1e0b7", "06000000000000007365727665720600000000000000636c69656e74410000000000000004a56fa807caaa53a4d28dbb9853b9815c61a411118a6fe516a8798434751470f9010153ac33d0d5f2047ffdb1a3e42c9b4e6be662766e1eeb4116988ede5f912c41000000000000000406557e482bd03097ad0cbaa5df82115460d951e3451962f1eaf4367a420676d09857ccbc522686c83d1852abfa8ed6e4a1155cf8f1543ceca528afb591a1e0b741000000000000000412af7e89717850671913e6b469ace67bd90a4df8ce45c2af19010175e37eed69f75897996d539356e2fa6a406d528501f907e04d97515fbe83db277b715d332520000000000000002ee57912099d31560b3a44b1184b9b4866e904c49d12ac5042c97dca461b1a5f", "0e0672dc86f8e45565d338b0540abe69", "58ad4aa88e0b60d5061eb6b5dd93e80d9c4f00d127c65b3b35b1b5281fee38f0", "d3e2e547f1ae04f2dbdbf0fc4b79f8ecff2dff314b5d32fe9fcef2fb26dc459b"},
	{"", "client", "0548d8729f730589e579b0475a582c1608138ddf7054b73b5381c7e883e2efae", "403abbe3b1b4b9ba17e3032849759d723939a27a27b9d921c500edde18ed654b", "903023b6598908936ea7c929bd761af6039577a9c3f9581064187c3049d87065", "04a897b769e681c62ac1c2357319a3d363f610839c4477720d24cbe32f5fd85f44fb92ba966578c1b712be6962498834078262caa5b441ecfa9d4a9485720e918a", "04e0f816fd1c35e22065d5556215c097e799390d16661c386e0ecc84593974a61b881a8c82327687d0501862970c64565560cb5671f696048050ca66ca5f8cc7fc", "00000000000000000600000000000000636c69656e74410000000000000004a897b769e681c62ac1c2357319a3d363f610839c4477720d24cbe32f5fd85f44fb92ba966578c1b712be6962498834078262caa5b441ecfa9d4a9485720e918a410000000000000004e0f816fd1c35e22065d5556215c097e799390d16661c386e0ecc84593974a61b881a8c82327687d0501862970c64565560cb5671f696048050ca66ca5f8cc7fc4100000000000000048f83ec9f6e4f87cc6f9dc740bdc2769725f923364f01c84148c049a39a735ebda82eac03e00112fd6a5710682767cff5361f7e819e53d8d3c3a2922e0d837aa620000000000000000548d8729f730589e579b0475a582c1608138ddf7054b73b5381c7e883e2efae", "642f05c473c2cd79909f9a841e2f30a7", "47d29e6666af1b7dd450d571233085d7a9866e4d49d2645e2df975489521232b", "3313c5cefc361d27fb16847a91c2a73b766ffa90a4839122a9b70a2f6bd1d6df"},
	{"server", "", "626e0cdc7b14c9db3e52a0b1b3a768c98e37852d5db30febe0497b14eae8c254", "07adb3db6bc623d3399726bfdbfd3d15a58ea776ab8a308b00392621291f9633", "b6a4fc8dbb629d4ba51d6f91ed1532cf87adec98f25dd153a75accafafedec16", "04f88fb71c99bfffaea370966b7eb99cd4be0ff1a7d335caac4211c4afd855e2e15a873b298503ad8ba1d9cbb9a392d2ba309b48bfd7879aefd0f2cea6009763b0", "040c269d6be017dccb15182ac6bfcd9e2a14de019dd587eaf4bdfd353f031101e7cca177f8eb362a6e83e7d5e729c0732e1b528879c086f39ba0f31a9661bd34db", "06000000000000007365727665720000000000000000410000000000000004f88fb71c99bfffaea370966b7eb99cd4be0ff1a7d335caac4211c4afd855e2e15a873b298503ad8ba1d9cbb9a392d2ba309b48bfd7879aefd0f2cea6009763b04100000000000000040c269d6be017dccb15182ac6bfcd9e2a14de019dd587eaf4bdfd353f031101e7cca177f8eb362a6e83e7d5e729c0732e1b528879c086f39ba0f31a9661bd34db41000000000000000445ee233b8ecb51ebd6e7da3f307e88a1616bae2166121221fdc0dadb986afaf3ec8a988dc9c626fa3b99f58a7ca7c9b844bb3e8dd9554aafc5b53813504c1cbe2000000000000000626e0cdc7b14c9db3e52a0b1b3a768c98e37852d5db30febe0497b14eae8c254", "005184ff460da2ce59062c87733c299c", "bc9f9bbe99f26d0b2260e6456e05a86196a3307ec6663a18bf6ac825736533b2", "c2370e1bf813b086dff0d834e74425a06e6390f48f5411900276dcccc5a297ec"},
	{"", "", "7bf46c454b4c1b25799527d896508afd5fc62ef4ec59db1efb49113063d70cca", "8cef65df64bb2d0f83540c53632de911b5b24b3eab6cc74a97609fd659e95473", "d7a66f64074a84652d8d623a92e20c9675c61cb5b4f6a0063e4648a2fdc02d53", "04a65b367a3f613cf9f0654b1b28a1e3a8a40387956c8ba6063e8658563890f46ca1ef6a676598889fc28de2950ab8120b79a5ef1ea4c9f44bc98f585634b46d66", "04589f13218822710d98d8b2123a079041052d9941b9cf88c6617ddb2fcc0494662eea8ba6b64692dc318250030c6af045cb738bc81ba35b043c3dcb46adf6f58d", "00000000000000000000000000000000410000000000000004a65b367a3f613cf9f0654b1b28a1e3a8a40387956c8ba6063e8658563890f46ca1ef6a676598889fc28de2950ab8120b79a5ef1ea4c9f44bc98f585634b46d66410000000000000004589f13218822710d98d8b2123a079041052d9941b9cf88c6617ddb2fcc0494662eea8ba6b64692dc318250030c6af045cb738bc81ba35b043c3dcb46adf6f58d4100000000000000041a3c03d51b452537ca2a1fea6110353c6d5ed483c4f0f86f4492ca3f378d40a994b4477f93c64d928edbbcd3e85a7c709b7ea73ee97986ce3d1438e13554377220000000000000007bf46c454b4c1b25799527d896508afd5fc62ef4ec59db1efb49113063d70cca", "fc6374762ba5cf11f4b2caa08b2cd1b9", "dfb4db8d48ae5a675963ea5e6c19d98d4ea028d8e898dad96ea19a80ade95dca", "d0f0609d1613138d354f7e95f19fb556bf52d751947241e8c7118df5ef0ae175"},
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRFC9382Vectors(t *testing.T) {
	for _, v := range vectors {
		idA, idB, w := []byte(v[0]), []byte(v[1]), unhex(t, v[2])
		a, pA, err := newWithScalar(RoleA, idA, idB, w, unhex(t, v[3]))
		if err != nil {
			t.Fatal(err)
		}
		b, pB, err := newWithScalar(RoleB, idA, idB, w, unhex(t, v[4]))
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(pA) != v[5] || hex.EncodeToString(pB) != v[6] {
			t.Fatalf("A=%q B=%q: messages differ", v[0], v[1])
		}
		ka, err := a.Finish(pB, nil)
		if err != nil {
			t.Fatal(err)
		}
		kb, err := b.Finish(pA, nil)
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(ka.Ke) != v[8] || !bytes.Equal(ka.Ke, kb.Ke) {
			t.Fatalf("A=%q B=%q: Ke differs", v[0], v[1])
		}
		if hex.EncodeToString(ka.MyConfirm) != v[9] || hex.EncodeToString(kb.MyConfirm) != v[10] {
			t.Fatalf("A=%q B=%q: confirmations differ", v[0], v[1])
		}
		if !ka.Verify(kb.MyConfirm) || !kb.Verify(ka.MyConfirm) {
			t.Fatal("confirmations do not verify")
		}
	}
}

func TestWrongPasswordAndBadPoint(t *testing.T) {
	a, pA, err := New(RoleA, []byte("joiner"), []byte("owner"), Password("d", []byte("right")))
	if err != nil {
		t.Fatal(err)
	}
	b, pB, err := New(RoleB, []byte("joiner"), []byte("owner"), Password("d", []byte("wrong")))
	if err != nil {
		t.Fatal(err)
	}
	ka, err := a.Finish(pB, []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	kb, err := b.Finish(pA, []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	if ka.Verify(kb.MyConfirm) || kb.Verify(ka.MyConfirm) || bytes.Equal(ka.Ke, kb.Ke) {
		t.Fatal("different passwords must not agree")
	}
	c, _, _ := New(RoleA, nil, nil, Password("d", []byte("x")))
	bad := append([]byte{4}, bytes.Repeat([]byte{1}, 64)...)
	if _, err := c.Finish(bad, nil); err != ErrBadMessage {
		t.Fatalf("off-curve point accepted: %v", err)
	}
	if _, err := c.Finish(pB, nil); err == nil {
		t.Fatal("state reused")
	}
}

func TestZeroPasswordRejected(t *testing.T) {
	if _, _, err := New(RoleA, nil, nil, make([]byte, 32)); err == nil {
		t.Fatal("zero w accepted")
	}
}
