// Copyright 2022 The Witness Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dsse

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"github.com/in-toto/go-witness/cryptoutil"
	"github.com/in-toto/go-witness/timestamp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createRsaKey() (*rsa.PrivateKey, *rsa.PublicKey, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	return privKey, &privKey.PublicKey, nil
}

func createTestKey() (cryptoutil.Signer, cryptoutil.Verifier, error) {
	privKey, pubKey, err := createRsaKey()
	if err != nil {
		return nil, nil, err
	}

	signer := cryptoutil.NewRSASigner(privKey, crypto.SHA256)
	verifier := cryptoutil.NewRSAVerifier(pubKey, crypto.SHA256)
	return signer, verifier, nil
}

func createCert(priv, pub interface{}, temp, parent *x509.Certificate) (*x509.Certificate, error) {
	var err error
	temp.SerialNumber, err = rand.Int(rand.Reader, big.NewInt(4294967295))
	if err != nil {
		return nil, err
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, temp, parent, pub, priv)
	if err != nil {
		return nil, err
	}

	return x509.ParseCertificate(certBytes)
}

func createRoot() (*x509.Certificate, interface{}, error) {
	priv, pub, err := createRsaKey()
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		Subject: pkix.Name{
			Country:      []string{"US"},
			Organization: []string{"TestifySec"},
			CommonName:   "Test Root",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        false,
		MaxPathLen:            2,
	}

	cert, err := createCert(priv, pub, template, template)
	return cert, priv, err
}

func createIntermediate(parent *x509.Certificate, parentPriv interface{}) (*x509.Certificate, interface{}, error) {
	priv, pub, err := createRsaKey()
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		Subject: pkix.Name{
			Country:      []string{"US"},
			Organization: []string{"TestifySec"},
			CommonName:   "Test Intermediate",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        false,
		MaxPathLen:            1,
	}

	cert, err := createCert(parentPriv, pub, template, parent)
	return cert, priv, err
}

func createLeaf(parent *x509.Certificate, parentPriv interface{}) (*x509.Certificate, interface{}, error) {
	priv, pub, err := createRsaKey()
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		Subject: pkix.Name{
			Country:      []string{"US"},
			Organization: []string{"TestifySec"},
			CommonName:   "Test Leaf",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	cert, err := createCert(parentPriv, pub, template, parent)
	return cert, priv, err
}

func TestSign(t *testing.T) {
	signer, _, err := createTestKey()
	require.NoError(t, err)
	_, err = Sign("dummydata", bytes.NewReader([]byte("this is some dummy data")), SignWithSigners(signer))
	require.NoError(t, err)
}

func TestVerify(t *testing.T) {
	signer, verifier, err := createTestKey()
	require.NoError(t, err)
	env, err := Sign("dummydata", bytes.NewReader([]byte("this is some dummy data")), SignWithSigners(signer))
	require.NoError(t, err)
	approvedVerifiers, err := env.Verify(VerifyWithVerifiers(verifier))
	assert.ElementsMatch(t, approvedVerifiers, []CheckedVerifier{{Verifier: verifier}})
	require.NoError(t, err)
}

func TestFailVerify(t *testing.T) {
	signer, _, err := createTestKey()
	require.NoError(t, err)
	_, verifier, err := createTestKey()
	require.NoError(t, err)
	env, err := Sign("dummydata", bytes.NewReader([]byte("this is some dummy data")), SignWithSigners(signer))
	require.NoError(t, err)
	approvedVerifiers, err := env.Verify(VerifyWithVerifiers(verifier))
	assert.Empty(t, approvedVerifiers)
	require.ErrorAs(t, err, &ErrNoMatchingSigs{Verifiers: []CheckedVerifier{{Verifier: verifier, Error: rsa.ErrVerification}}})
}

func TestMultiSigners(t *testing.T) {
	signers := []cryptoutil.Signer{}
	verifiers := []cryptoutil.Verifier{}
	expectedVerifiers := []CheckedVerifier{}
	for i := 0; i < 5; i++ {
		s, v, err := createTestKey()
		require.NoError(t, err)
		signers = append(signers, s)
		verifiers = append(verifiers, v)
		expectedVerifiers = append(expectedVerifiers, CheckedVerifier{Verifier: v})
	}

	env, err := Sign("dummydata", bytes.NewReader([]byte("this is some dummy data")), SignWithSigners(signers...))
	require.NoError(t, err)

	checkedVerifiers, err := env.Verify(VerifyWithVerifiers(verifiers...))
	approvedVerifiers := []CheckedVerifier{}
	for _, v := range checkedVerifiers {
		if v.Error == nil {
			approvedVerifiers = append(approvedVerifiers, v)
		}
	}
	require.NoError(t, err)
	assert.ElementsMatch(t, approvedVerifiers, expectedVerifiers)
}

func TestThreshold(t *testing.T) {
	signers := []cryptoutil.Signer{}
	expectedVerifiers := []CheckedVerifier{}
	verifiers := []cryptoutil.Verifier{}
	for i := 0; i < 5; i++ {
		s, v, err := createTestKey()
		require.NoError(t, err)
		signers = append(signers, s)
		expectedVerifiers = append(expectedVerifiers, CheckedVerifier{Verifier: v})
		verifiers = append(verifiers, v)
	}

	// create some additional verifiers that won't be used to sign
	for i := 0; i < 5; i++ {
		_, v, err := createTestKey()
		require.NoError(t, err)
		verifiers = append(verifiers, v)
	}

	env, err := Sign("dummydata", bytes.NewReader([]byte("this is some dummy data")), SignWithSigners(signers...))
	require.NoError(t, err)

	checkedVerifiers, err := env.Verify(VerifyWithVerifiers(verifiers...), VerifyWithThreshold(5))
	require.NoError(t, err)

	approvedVerifiers := []CheckedVerifier{}
	for _, v := range checkedVerifiers {
		if v.Error == nil {
			approvedVerifiers = append(approvedVerifiers, v)
		}
	}
	assert.ElementsMatch(t, approvedVerifiers, expectedVerifiers)

	checkedVerifiers, err = env.Verify(VerifyWithVerifiers(verifiers...), VerifyWithThreshold(10))
	require.ErrorIs(t, err, ErrThresholdNotMet{Actual: 5, Theshold: 10})

	approvedVerifiers = []CheckedVerifier{}
	for _, v := range checkedVerifiers {
		if v.Error == nil {
			approvedVerifiers = append(approvedVerifiers, v)
		}
	}
	assert.ElementsMatch(t, approvedVerifiers, expectedVerifiers)

	_, err = env.Verify(VerifyWithVerifiers(verifiers...), VerifyWithThreshold(-10))
	require.ErrorIs(t, err, ErrInvalidThreshold(-10))
}

func TestTimestamp(t *testing.T) {
	root, rootPriv, err := createRoot()
	require.NoError(t, err)
	intermediate, intermediatePriv, err := createIntermediate(root, rootPriv)
	require.NoError(t, err)
	leaf, leafPriv, err := createLeaf(intermediate, intermediatePriv)
	require.NoError(t, err)
	s, err := cryptoutil.NewSigner(leafPriv, cryptoutil.SignWithCertificate(leaf))
	require.NoError(t, err)
	v, err := s.Verifier()
	require.NoError(t, err)
	expectedTimestampers := []timestamp.FakeTimestamper{
		{T: time.Now()},
		{T: time.Now().Add(12 * time.Hour)},
	}
	unexpectedTimestampers := []timestamp.FakeTimestamper{
		{T: time.Now().Add(36 * time.Hour)},
		{T: time.Now().Add(128 * time.Hour)},
	}

	allTimestampers := make([]timestamp.Timestamper, 0)
	allTimestampVerifiers := make([]timestamp.TimestampVerifier, 0)
	for _, expected := range expectedTimestampers {
		allTimestampers = append(allTimestampers, expected)
		allTimestampVerifiers = append(allTimestampVerifiers, expected)
	}

	for _, unexpected := range unexpectedTimestampers {
		allTimestampers = append(allTimestampers, unexpected)
		allTimestampVerifiers = append(allTimestampVerifiers, unexpected)
	}

	env, err := Sign("dummydata", bytes.NewReader([]byte("this is some dummy data")), SignWithSigners(s), SignWithTimestampers(allTimestampers...))
	require.NoError(t, err)

	checkedVerifiers, err := env.Verify(VerifyWithVerifiers(v), VerifyWithRoots(root), VerifyWithIntermediates(intermediate), VerifyWithTimestampVerifiers(allTimestampVerifiers...))
	require.NoError(t, err)

	approvedVerifiers := []CheckedVerifier{}
	for _, v := range checkedVerifiers {
		if v.Error == nil {
			approvedVerifiers = append(approvedVerifiers, v)
		}
	}
	assert.Len(t, approvedVerifiers, 1)
	assert.Len(t, approvedVerifiers[0].TimestampVerifiers, len(expectedTimestampers))
	assert.ElementsMatch(t, approvedVerifiers[0].TimestampVerifiers, expectedTimestampers)
}
