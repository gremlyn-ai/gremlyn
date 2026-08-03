"use client";

import Image from "next/image";
import Link from "next/link";

type NavItem = {
  href: string;
  icon: string;
  label: string;
  page: string;
  iconFill?: boolean;
};

const navItems: NavItem[] = [
  { href: "/arena", icon: "bolt", label: "Arena", page: "arena" },
  { href: "/shield", icon: "shield", label: "Shield", page: "shield", iconFill: true },
  { href: "/servers", icon: "dns", label: "Servers", page: "servers" },
  { href: "/settings", icon: "settings", label: "Settings", page: "settings" },
  { href: "/docs", icon: "description", label: "Docs", page: "docs" },
  { href: "/about", icon: "info", label: "About", page: "about" },
];

type SidebarProps = {
  activePage: "arena" | "shield" | "servers" | "settings" | "docs" | "about";
};

export function Sidebar({ activePage }: SidebarProps) {
  const accentHex = activePage === "arena" ? "#ff7168" : "#8eff71";

  return (
    <aside className="fixed left-0 top-0 h-full flex flex-col z-40 bg-[#000000] w-64 border-r-0 font-mono text-xs uppercase tracking-tight">
      {/* Logo */}
      <div className="p-6 mb-8">
        <div className="flex items-center gap-3">
          <div className="relative w-12 h-12 bg-surface-container-low flex items-center justify-center">
            <Image
              src="/gremlyn-white.png"
              alt="Gremlyn"
              width={40}
              height={40}
              className="object-contain"
            />
          </div>
          <div>
            <h1 className="font-bold text-lg leading-none" style={{ color: accentHex }}>
              GREMLYN_SYS
            </h1>
            <p className="text-[#555555] text-[10px] mt-1">beta</p>
          </div>
        </div>
      </div>

      {/* Navigation */}
      <nav className="flex-1 space-y-1">
        {navItems.map((item) => {
          const isActive = item.page === activePage;
          const activeAccent =
            item.page === "arena" ? "#ff7168" : "#8eff71";

          return (
            <Link
              key={item.page}
              href={item.href}
              className={`group flex items-center px-6 py-4 transition-all duration-150 ${
                isActive
                  ? `bg-[#262626] border-l-4`
                  : "text-[#555555] hover:text-[#8eff71] hover:bg-[#131313]"
              }`}
              style={isActive ? { color: activeAccent, borderColor: activeAccent } : undefined}
            >
              <span
                className="material-symbols-outlined mr-4 group-hover:translate-x-1 transition-transform"
                style={
                  isActive && item.iconFill
                    ? { fontVariationSettings: "'FILL' 1" }
                    : undefined
                }
              >
                {item.icon}
              </span>
              {item.label}
            </Link>
          );
        })}
      </nav>

      <div className="mt-auto border-t border-outline-variant/10 p-2">
      </div>
    </aside>
  );
}
