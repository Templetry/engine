// tpl:if router
import { Routes } from "./routes";
// tpl:endif

export function TemplateApp() {
  // tpl:if router
  return <Routes />;
  // tpl:endif
  // tpl:if !router
  return <main>template-app</main>;
  // tpl:endif
}
