package i18n

import "testing"

// appendixA holds the texts of Appendix A of the plugin security draft: the consent screens and the
// words of the command line, in English and in French. The state names that hold a dash in the
// draft use an underscore here, because a catalogue key only holds letters, digits and
// underscores. Non-ASCII characters are written as escapes: \u00a0 is the no-break space the
// French typography puts before a colon or a question mark, and \u2019 the apostrophe.
var appendixA = []struct{ key, en, fr string }{
	{
		"consent_title_install",
		"Install plugin %s %s?",
		"Installer le plugin %s %s\u00a0?",
	},
	{
		"consent_title_update",
		"Update plugin %s from %s to %s?",
		"Mettre \u00e0 jour le plugin %s de %s vers %s\u00a0?",
	},
	{
		"consent_title_escalation",
		"%s %s asks for more access",
		"%s %s demande plus d\u2019acc\u00e8s",
	},
	{
		"consent_title_allow",
		"Allow plugin %s %s to run?",
		"Autoriser le plugin %s %s \u00e0 s\u2019ex\u00e9cuter\u00a0?",
	},
	{
		"consent_source",
		"Source",
		"Source",
	},
	{
		"consent_origin_official",
		"official plugin of the tote project",
		"plugin officiel du projet tote",
	},
	{
		"consent_origin_community",
		"community plugin",
		"plugin de la communaut\u00e9",
	},
	{
		"consent_origin_local",
		"local files, not verified",
		"fichiers locaux, non v\u00e9rifi\u00e9s",
	},
	{
		"consent_origin_embedded",
		"shipped with tote",
		"livr\u00e9 avec tote",
	},
	{
		"consent_checksum",
		"Checksum",
		"Somme de contr\u00f4le",
	},
	{
		"consent_checksum_pinned",
		"sha256 %s, typed by you with --sha256",
		"sha256 %s, saisie par vous avec --sha256",
	},
	{
		"consent_checksum_embedded",
		"sha256 %s, covered by the signature of this tote release",
		"sha256 %s, couverte par la signature de cette version de tote",
	},
	{
		"consent_checksum_local",
		"sha256 %s, computed here, nothing to compare it with",
		"sha256 %s, calcul\u00e9e ici, rien \u00e0 comparer",
	},
	{
		"consent_code_declarative",
		"No code of its own: tote runs only the programs listed below.",
		"Aucun code propre\u00a0: tote ne lance que les programmes list\u00e9s ci-dessous.",
	},
	{
		"consent_code_exec",
		"Runs its own program %s (code written by the plugin author).",
		"Lance son propre programme %s (code \u00e9crit par l\u2019auteur du plugin).",
	},
	{
		"consent_description_head",
		"Description (written by the plugin, not checked by tote)",
		"Description (\u00e9crite par le plugin, non v\u00e9rifi\u00e9e par tote)",
	},
	{
		"consent_can_head",
		"This plugin will be able to:",
		"Ce plugin pourra\u00a0:",
	},
	{
		"consent_none",
		"nothing beyond showing its own entries",
		"rien de plus que d\u2019afficher ses propres entr\u00e9es",
	},
	{
		"consent_cap_tty",
		"take over the terminal while some actions run (it can read keys and draw anything)",
		"prendre le contr\u00f4le du terminal pendant certaines actions (il peut lire les " +
			"touches et tout afficher)",
	},
	{
		"consent_cap_exec",
		"run the program %s",
		"lancer le programme %s",
	},
	{
		"consent_cap_exec_broad",
		"run any program through %s",
		"lancer n\u2019importe quel programme via %s",
	},
	{
		"consent_cap_net",
		"contact %s (declared; tote cannot block it)",
		"contacter %s (d\u00e9clar\u00e9\u00a0; tote ne peut pas le bloquer)",
	},
	{
		"consent_cap_fs_read",
		"read files: %s",
		"lire des fichiers\u00a0: %s",
	},
	{
		"consent_cap_fs_write",
		"write files: %s",
		"\u00e9crire des fichiers\u00a0: %s",
	},
	{
		"consent_cap_env",
		"read the environment variable %s",
		"lire la variable d\u2019environnement %s",
	},
	{
		"consent_cap_env_sensitive",
		"read the environment variable %s (may hold a secret)",
		"lire la variable d\u2019environnement %s (peut contenir un secret)",
	},
	{
		"consent_cap_token",
		"receive the token you configure (from a variable or a command)",
		"recevoir le jeton que vous configurez (depuis une variable ou une commande)",
	},
	{
		"consent_cap_read_conn",
		"read your connection list (names, hosts, users)",
		"lire votre liste de connexions (noms, h\u00f4tes, utilisateurs)",
	},
	{
		"consent_cap_read_repos",
		"read your repository list (names, paths, remotes)",
		"lire votre liste de d\u00e9p\u00f4ts (noms, chemins, remotes)",
	},
	{
		"consent_cap_handoff",
		"propose a command for your shell prompt",
		"proposer une commande pour votre invite de shell",
	},
	{
		"consent_cap_openurl",
		"ask to open web addresses (you confirm each one)",
		"demander l\u2019ouverture d\u2019adresses web (vous confirmez chacune)",
	},
	{
		"consent_cap_editor",
		"open files in your editor",
		"ouvrir des fichiers dans votre \u00e9diteur",
	},
	{
		"consent_cap_clipboard",
		"copy text to your clipboard",
		"copier du texte dans votre presse-papiers",
	},
	{
		"consent_cap_notify",
		"show messages in the status line",
		"afficher des messages dans la ligne d\u2019\u00e9tat",
	},
	{
		"consent_missing_dep",
		"Missing on this machine: %s",
		"Absent de cette machine\u00a0: %s",
	},
	{
		"consent_hint",
		"Hint from the plugin: %s",
		"Indication du plugin\u00a0: %s",
	},
	{
		"consent_warn_exec",
		"This plugin runs with all your user rights. tote cannot limit what its own program " +
			"does; the list above is what it declares.",
		"Ce plugin s\u2019ex\u00e9cute avec tous vos droits. tote ne peut pas limiter ce que " +
			"fait son propre programme\u00a0; la liste ci-dessus est ce qu\u2019il d\u00e9clare.",
	},
	{
		"consent_warn_declarative",
		"tote runs only the listed programs, but those programs can do whatever you can do " +
			"with them.",
		"tote ne lance que les programmes list\u00e9s, mais ces programmes peuvent faire tout " +
			"ce que vous pouvez faire avec eux.",
	},
	{
		"consent_warn_similar",
		"The name looks like the plugin %s that you already have.",
		"Le nom ressemble au plugin %s que vous avez d\u00e9j\u00e0.",
	},
	{
		"consent_warn_source_changed",
		"Installed from %s, now offered by %s.",
		"Install\u00e9 depuis %s, propos\u00e9 maintenant par %s.",
	},
	{
		"consent_diff_added",
		"New access:",
		"Nouveaux acc\u00e8s\u00a0:",
	},
	{
		"consent_diff_kept",
		"Unchanged: %d",
		"Inchang\u00e9s\u00a0: %d",
	},
	{
		"consent_diff_removed",
		"No longer asked: %d",
		"Plus demand\u00e9s\u00a0: %d",
	},
	{
		"consent_diff_cmds",
		"Commands: %d added, %d removed, %d changed",
		"Commandes\u00a0: %d ajout\u00e9es, %d retir\u00e9es, %d modifi\u00e9es",
	},
	{
		"consent_code_line",
		"Consent code %s",
		"Code de consentement %s",
	},
	{
		"consent_btn_cancel",
		"Cancel",
		"Annuler",
	},
	{
		"consent_btn_install",
		"Install and enable",
		"Installer et activer",
	},
	{
		"consent_btn_update",
		"Update",
		"Mettre \u00e0 jour",
	},
	{
		"consent_btn_allow",
		"Allow",
		"Autoriser",
	},
	{
		"plugin_installed",
		"installed plugin %s %s (%s)",
		"plugin %s %s install\u00e9 (%s)",
	},
	{
		"plugin_updated_quiet",
		"updated %s from %s to %s, permissions unchanged",
		"%s mis \u00e0 jour de %s vers %s, permissions inchang\u00e9es",
	},
}

// TestAppendixAVerbatim checks that every text of Appendix A is in both catalogues word for word.
// It also guards the keys that only a later screen uses.
//
// Parameters:
//   - t: test to report to
func TestAppendixAVerbatim(t *testing.T) {
	en, fr := catalogs(t)

	for _, row := range appendixA {
		if got := en[row.key]; got != row.en {
			t.Errorf("en %s = %q, want %q", row.key, got, row.en)
		}

		if got := fr[row.key]; got != row.fr {
			t.Errorf("fr %s = %q, want %q", row.key, got, row.fr)
		}
	}
}
