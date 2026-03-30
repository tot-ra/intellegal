import { NavLink, Outlet } from "react-router-dom";

type NavItem = {
  to: string;
  label: string;
  end?: boolean;
};

const navItems: NavItem[] = [
  { to: "/", label: "Dashboard", end: true },
  { to: "/contracts", label: "Contracts" },
  { to: "/checks", label: "Checks" },
  { to: "/results", label: "Results" },
  { to: "/audit", label: "Audit Log" }
];

export function AppShell() {
  return (
    <div className="app-shell">
      <header className="app-header">
        <div className="brand">
          <img className="logo" src="/logo.webp" alt="IntelLegal logo" />
          <div className="brand-content">
            <p className="brand-kicker">IntelLegal</p>
            <h1>Legal Document Intelligence</h1>
            <nav className="nav" aria-label="Primary">
              {navItems.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.end}
                  className={({ isActive }) => (isActive ? "active" : undefined)}
                >
                  {item.label}
                </NavLink>
              ))}
            </nav>
          </div>
        </div>
      </header>
      <main className="app-main">
        <Outlet />
      </main>
    </div>
  );
}
