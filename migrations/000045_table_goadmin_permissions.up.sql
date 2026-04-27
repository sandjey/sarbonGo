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
-- Name: goadmin_permissions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_permissions (
    id integer NOT NULL,
    name character varying(50) NOT NULL,
    slug character varying(50) NOT NULL,
    http_method character varying(255),
    http_path text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_permissions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.goadmin_permissions_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: goadmin_permissions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.goadmin_permissions_id_seq OWNED BY public.goadmin_permissions.id;


--
-- Name: goadmin_permissions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_permissions ALTER COLUMN id SET DEFAULT nextval('public.goadmin_permissions_id_seq'::regclass);


--
-- Name: goadmin_permissions goadmin_permissions_name_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_permissions
    ADD CONSTRAINT goadmin_permissions_name_unique UNIQUE (name);


--
-- Name: goadmin_permissions goadmin_permissions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_permissions
    ADD CONSTRAINT goadmin_permissions_pkey PRIMARY KEY (id);


--
-- PostgreSQL database dump complete
--


