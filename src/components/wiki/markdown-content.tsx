import ReactMarkdown, { type UrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";

import styles from "./markdown-content.module.css";

type MarkdownContentProps = {
  body: string;
  urlTransform?: UrlTransform;
};

export function MarkdownContent({ body, urlTransform }: MarkdownContentProps) {
  return (
    <div className={styles.markdown}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml urlTransform={urlTransform}>
        {body}
      </ReactMarkdown>
    </div>
  );
}
