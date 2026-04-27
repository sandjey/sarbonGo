--
-- PostgreSQL database dump
--


-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: goadmin_menu; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_menu (
    id integer NOT NULL,
    parent_id integer DEFAULT 0 NOT NULL,
    type smallint DEFAULT 0 NOT NULL,
    "order" integer DEFAULT 0 NOT NULL,
    title character varying(50) NOT NULL,
    icon character varying(50) NOT NULL,
    uri character varying(3000) DEFAULT ''::character varying NOT NULL,
    header character varying(150),
    plugin_name character varying(150) DEFAULT ''::character varying NOT NULL,
    uuid character varying(150),
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_menu_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.goadmin_menu_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: goadmin_menu_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.goadmin_menu_id_seq OWNED BY public.goadmin_menu.id;


--
-- Name: goadmin_menu id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_menu ALTER COLUMN id SET DEFAULT nextval('public.goadmin_menu_id_seq'::regclass);


--
-- Name: goadmin_menu goadmin_menu_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_menu
    ADD CONSTRAINT goadmin_menu_pkey PRIMARY KEY (id);


--
-- PostgreSQL database dump complete
--


