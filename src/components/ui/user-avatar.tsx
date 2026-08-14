import styles from "./user-avatar.module.css";

type UserAvatarProps = {
  name: string;
  avatarUrl?: string | null;
  size?: number;
  shape?: "circle" | "square";
};

export function UserAvatar({ name, avatarUrl, size = 20, shape = "circle" }: UserAvatarProps) {
  const dimension = { width: size, height: size };
  if (avatarUrl) {
    return (
      // eslint-disable-next-line @next/next/no-img-element
      <img
        alt=""
        aria-hidden="true"
        className={styles.avatar}
        data-shape={shape}
        referrerPolicy="no-referrer"
        src={avatarUrl}
        {...dimension}
      />
    );
  }
  return (
    <span
      aria-hidden="true"
      className={styles.fallback}
      data-shape={shape}
      style={{ ...dimension, fontSize: Math.max(10, size * 0.45) }}
    >
      {name.slice(0, 1).toUpperCase()}
    </span>
  );
}
