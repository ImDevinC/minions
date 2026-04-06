"use client";

import { signIn } from "next-auth/react";

export default function SignInPage() {
  return (
    <main className="min-h-screen bg-gray-900 flex items-center justify-center">
      <div className="bg-gray-800 p-8 rounded-lg shadow-xl max-w-md w-full">
        <h1 className="text-2xl font-bold text-white text-center mb-6">
          Minions Control Panel
        </h1>
        <p className="text-gray-400 text-center mb-8">
          Sign in to manage your minions
        </p>
        <button
          onClick={() => signIn("oidc", { callbackUrl: "/" })}
          className="w-full bg-blue-600 hover:bg-blue-700 text-white py-3 px-4 rounded-md font-medium transition-colors flex items-center justify-center gap-2"
        >
          <svg
            className="w-5 h-5"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
            xmlns="http://www.w3.org/2000/svg"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z"
            />
          </svg>
          Sign In
        </button>
      </div>
    </main>
  );
}
