import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import styles from "./markdown-content.module.css";

type MarkdownContentProps = {
  body: string;
};

export function MarkdownContent({ body }: MarkdownContentProps) {
  return (
    <div className={styles.markdown}>
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{body}</ReactMarkdown>
    </div>
  );
}
