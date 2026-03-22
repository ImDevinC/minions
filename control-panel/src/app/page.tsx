import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/auth";
import { redirect } from "next/navigation";
import { SignOutButton } from "@/components/sign-out-button";

export default async function Home() {
  const session = await getServerSession(authOptions);

  if (!session) {
    redirect("/api/auth/signin");
  }

  return (
    <main className="min-h-screen bg-gray-900 text-white p-8">
      <div className="max-w-6xl mx-auto">
        <div className="flex justify-between items-center mb-8">
          <h1 className="text-3xl font-bold">Minions Control Panel</h1>
          <div className="flex items-center gap-4">
            <span className="text-gray-400">
              {session.user?.name}
            </span>
            <SignOutButton />
          </div>
        </div>
        <p className="text-gray-400">
          Welcome to the control panel. Dashboard coming soon.
        </p>
      </div>
    </main>
  );
}
