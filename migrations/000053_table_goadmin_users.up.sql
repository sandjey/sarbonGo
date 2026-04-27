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
-- Name: goadmin_users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_users (
    id integer NOT NULL,
    username character varying(100) NOT NULL,
    password character varying(100) DEFAULT ''::character varying NOT NULL,
    name character varying(100) NOT NULL,
    avatar character varying(255),
    remember_token character varying(100),
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_users_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.goadmin_users_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: goadmin_users_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.goadmin_users_id_seq OWNED BY public.goadmin_users.id;


--
-- Name: goadmin_users id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_users ALTER COLUMN id SET DEFAULT nextval('public.goadmin_users_id_seq'::regclass);


--
-- Name: goadmin_users goadmin_users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_users
    ADD CONSTRAINT goadmin_users_pkey PRIMARY KEY (id);


--
-- Name: goadmin_users goadmin_users_username_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_users
    ADD CONSTRAINT goadmin_users_username_unique UNIQUE (username);


--
-- PostgreSQL database dump complete
--


