package io.github.manugh.xg2g.android.auth

import android.content.Context
import androidx.credentials.CreateCredentialRequest
import androidx.credentials.CreatePublicKeyCredentialRequest
import androidx.credentials.CreatePublicKeyCredentialResponse
import androidx.credentials.CredentialManager
import androidx.credentials.GetCredentialRequest
import androidx.credentials.GetPublicKeyCredentialOption
import androidx.credentials.PublicKeyCredential
import androidx.credentials.exceptions.CreateCredentialCancellationException
import androidx.credentials.exceptions.GetCredentialCancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

interface NativePasskeyHandler {
    suspend fun getPasskeyAssertion(context: Context, requestJson: String): Result<String>
    suspend fun createPasskeyCredential(context: Context, requestJson: String): Result<String>
}

class AndroidCredentialManagerHandler : NativePasskeyHandler {

    override suspend fun getPasskeyAssertion(context: Context, requestJson: String): Result<String> =
        withContext(Dispatchers.IO) {
            try {
                val credentialManager = CredentialManager.create(context)
                val option = GetPublicKeyCredentialOption(requestJson)
                val request = GetCredentialRequest.Builder()
                    .addCredentialOption(option)
                    .build()

                val result = credentialManager.getCredential(context, request)
                val credential = result.credential
                if (credential is PublicKeyCredential) {
                    Result.success(credential.authenticationResponseJson)
                } else {
                    Result.failure(IllegalStateException("Received non-public key credential response"))
                }
            } catch (e: GetCredentialCancellationException) {
                Result.failure(IllegalStateException("User canceled biometric passkey dialog"))
            } catch (e: Throwable) {
                Result.failure(e)
            }
        }

    override suspend fun createPasskeyCredential(context: Context, requestJson: String): Result<String> =
        withContext(Dispatchers.IO) {
            try {
                val credentialManager = CredentialManager.create(context)
                val request: CreateCredentialRequest = CreatePublicKeyCredentialRequest(requestJson)
                val result = credentialManager.createCredential(context, request)

                if (result is CreatePublicKeyCredentialResponse) {
                    Result.success(result.registrationResponseJson)
                } else {
                    Result.failure(IllegalStateException("Received non-public key registration response"))
                }
            } catch (e: CreateCredentialCancellationException) {
                Result.failure(IllegalStateException("User canceled biometric passkey creation dialog"))
            } catch (e: Throwable) {
                Result.failure(e)
            }
        }
}
