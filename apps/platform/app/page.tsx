import PlatformEditor from "./components/PlatformEditor";
import UserMenu from "./components/UserMenu";

export default function Home() {
  return <PlatformEditor userMenu={<UserMenu />} />;
}
