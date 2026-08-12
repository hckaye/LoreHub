const concat = (...parts: string[]) => parts.join("");

export const actionsSettings = {
  en: {
    title: "Actions variables and secrets",
    organizationDescription: "Set values shared by Actions workflows in every repository in this organization.",
    repositoryDescription: "Set repository values or values limited to a named deployment environment.",
    scope: "Scope",
    repositoryScope: "Repository",
    environmentScope: "Environment",
    environmentName: "Environment name",
    environmentPlaceholder: "For example, production",
    loadEnvironment: "Load environment",
    environmentRequiredTitle: "Choose an environment",
    environmentRequired: "Enter an environment name to view and manage its variables and secrets.",
    name: "Name",
    type: "Type",
    value: "Value",
    variable: "Variable",
    secret: "Secret",
    createTitle: "Create a variable or secret",
    overwriteTitle: "Overwrite a variable or secret",
    variableValueHelp: "Variable values can be viewed by repository administrators and are passed to workflows.",
    secretValueHelp: "Secret values are accepted once and are never returned by the API or shown again.",
    saveVariable: "Save variable",
    saveSecret: "Save secret",
    saving: "Saving…",
    cancel: "Cancel",
    overwrite: "Overwrite",
    delete: "Delete",
    deleteConfirm: "Delete {name}? Workflows that use it may fail.",
    updated: "Updated",
    keyId: "Encryption key",
    secretStored: "Secret value hidden",
    actions: "Actions",
    emptyTitle: "No variables or secrets",
    emptyBody: "Create a variable or secret for this scope. Higher and lower scopes remain separate.",
    loading: "Loading Actions variables and secrets…",
    forbiddenTitle: "Administrator access is required",
    forbiddenBody: "Only an organization owner or an eligible repository administrator can manage these values.",
    unavailableTitle: "Actions settings are unavailable",
    unavailableBody: "The settings service could not be reached. Existing workflows are not changed.",
    retry: "Try again",
    mutationForbidden: "Your account does not have permission to change this Actions setting.",
    mutationUnavailable: "The Actions setting could not be changed. Try again when the service is available.",
    invalid: "Check the name and value. Reserved Actions and runner names cannot be used.",
    saved: "Actions setting saved.",
    deleted: "Actions setting deleted.",
  },
  ja: {
    title: "Actionsの変数とシークレット",
    // prettier-ignore
    organizationDescription: concat(
      "組織内の全リポジトリでActionsワークフローが",
      "共有する値を設定します。",
    ),
    // prettier-ignore
    repositoryDescription: concat(
      "リポジトリ全体、または指定したデプロイ環境だけで",
      "使う値を設定します。",
    ),
    scope: "適用範囲",
    repositoryScope: "リポジトリ",
    environmentScope: "環境",
    environmentName: "環境名",
    environmentPlaceholder: "例: production",
    loadEnvironment: "環境を読み込む",
    environmentRequiredTitle: "環境を選択してください",
    // prettier-ignore
    environmentRequired: concat(
      "環境名を入力すると、その環境の変数とシークレットを",
      "表示・管理できます。",
    ),
    name: "名前",
    type: "種類",
    value: "値",
    variable: "変数",
    secret: "シークレット",
    createTitle: "変数またはシークレットを作成",
    overwriteTitle: "変数またはシークレットを上書き",
    // prettier-ignore
    variableValueHelp: concat(
      "変数の値はリポジトリ管理者が確認でき、",
      "ワークフローへ渡されます。",
    ),
    // prettier-ignore
    secretValueHelp: concat(
      "シークレットの値は一度だけ受け取り、APIから返したり、",
      "画面へ再表示したりしません。",
    ),
    saveVariable: "変数を保存",
    saveSecret: "シークレットを保存",
    saving: "保存中…",
    cancel: "キャンセル",
    overwrite: "上書き",
    delete: "削除",
    // prettier-ignore
    deleteConfirm: concat(
      "{name}を削除しますか？ この値を使うワークフローが",
      "失敗する可能性があります。",
    ),
    updated: "更新日時",
    keyId: "暗号鍵",
    secretStored: "シークレットの値は非表示です",
    actions: "操作",
    emptyTitle: "変数とシークレットはありません",
    // prettier-ignore
    emptyBody: concat(
      "この適用範囲に変数またはシークレットを作成できます。",
      "別の適用範囲の値は変更しません。",
    ),
    loading: "Actionsの変数とシークレットを読み込んでいます…",
    forbiddenTitle: "管理者権限が必要です",
    // prettier-ignore
    forbiddenBody: concat(
      "組織の所有者、または条件を満たすリポジトリ管理者だけが",
      "値を管理できます。",
    ),
    unavailableTitle: "Actions設定を利用できません",
    // prettier-ignore
    unavailableBody: concat(
      "設定サービスに接続できませんでした。",
      "既存のワークフロー設定は変更されていません。",
    ),
    retry: "再試行",
    mutationForbidden: "このActions設定を変更する権限がありません。",
    // prettier-ignore
    mutationUnavailable: concat(
      "Actions設定を変更できませんでした。",
      "サービスの復旧後にもう一度お試しください。",
    ),
    // prettier-ignore
    invalid: concat(
      "名前と値を確認してください。",
      "Actionsとrunnerが予約している名前は使用できません。",
    ),
    saved: "Actions設定を保存しました。",
    deleted: "Actions設定を削除しました。",
  },
} as const;
